package cmd

import (
	"fmt"
	"time"

	apicontext "github.com/AwesomeCICD/circleci-org-migration-cli/api/context"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/settings"
	"github.com/spf13/cobra"
)

// GuardUnattendedCaptureAll fails closed when a non-interactive 'secrets
// capture' run would sweep EVERY context/project with values and trigger real
// extraction pipelines without an explicit opt-in (#164).
//
// It returns an error only when ALL of the following hold:
//   - wantsInteraction is false (no guided walkthrough — manifest/flags supplied
//     or stdin is not a TTY),
//   - neither --context nor --project was explicitly set (contextChanged and
//     projectChanged are both false), so capture would default to capture-all,
//   - and the caller passed no explicit unattended opt-in (neither --yes nor
//     --no-input).
//
// In every other case it returns nil and capture proceeds.
//
// It is exported so cmd_test can unit-test the guard directly without a live run.
func GuardUnattendedCaptureAll(wantsInteraction, contextChanged, projectChanged, assumeYes, noInput bool) error {
	if wantsInteraction {
		// Guided walkthrough will scope/confirm interactively.
		return nil
	}
	if contextChanged || projectChanged {
		// Caller scoped the capture explicitly.
		return nil
	}
	if assumeYes || noInput {
		// Caller explicitly acknowledged the unattended capture-all.
		return nil
	}
	return fmt.Errorf(
		"refusing to run an unattended capture-all: no --context or --project was given, " +
			"so capture would select EVERY context and project with values and trigger real " +
			"CircleCI extraction pipelines. Choose one of:\n" +
			"  1. pass --context and/or --project to scope exactly what is captured;\n" +
			"  2. pass --yes (or --no-input) to acknowledge an unattended capture-all;\n" +
			"  3. run on an interactive TTY (omit --manifest) for the guided walkthrough")
}

// captureFlags holds the bound flag values for 'secrets capture'. The RunE
// closure is extracted to (*captureFlags).run so newSecretsCaptureCommand stays
// focused on command/flag wiring.
type captureFlags struct {
	manifestPath          string
	output                string
	projectSlugs          []string
	contextNames          []string
	hostProjectSlug       string
	branch                string
	enableTrigger         bool
	skipRestrictedCtxs    bool
	removeRestrictions    bool
	noInput               bool
	assumeYes             bool // explicit opt-in to unattended capture-all (no --context/--project)
	noEncrypt             bool // explicit opt-out; overrides the encrypt=true default
	pollTimeout           time.Duration
	artifactRetentionDays int
	encOpts               captureEncryptOpts
	sshKeysCapture        bool // when true, extract SSH private keys for projects with cataloged keys
}

// newSecretsCaptureCommand builds the "secrets capture" subcommand. The RunE
// body and flag binding live on *captureFlags (see secrets_capture_run.go); the
// per-project orchestration lives in internal/capture.
func newSecretsCaptureCommand() *cobra.Command {
	cf := &captureFlags{}

	cmd := &cobra.Command{
		Use:   "capture [--manifest <file>]",
		Short: "Capture secret values by running an unversioned pipeline inside CircleCI (RECOMMENDED).",
		Long:  secretsCaptureLong,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cf.run(cmd)
		},
	}
	cf.bind(cmd.Flags())
	return cmd
}

// newOrgClientForCapture creates an *org.Client for reading and writing org
// feature flags during the capture flow.
func newOrgClientForCapture(cfg *settings.Config, token string) (*org.Client, error) {
	c, err := org.NewClient(cfg, token)
	if err != nil {
		return nil, fmt.Errorf("creating org client: %w", err)
	}
	return c, nil
}

// newContextClientForCapture creates an *apicontext.Client for managing
// context restrictions during the capture flow.
func newContextClientForCapture(cfg *settings.Config, token string) (*apicontext.Client, error) {
	c, err := apicontext.NewClient(cfg, token)
	if err != nil {
		return nil, fmt.Errorf("creating context client: %w", err)
	}
	return c, nil
}
