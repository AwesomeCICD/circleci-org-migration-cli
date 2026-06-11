package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	apicontext "github.com/AwesomeCICD/circleci-org-migration-cli/api/context"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	bundlepkg "github.com/AwesomeCICD/circleci-org-migration-cli/internal/bundle"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/clog"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/extract"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/settings"
	"github.com/AwesomeCICD/circleci-org-migration-cli/version"
	"github.com/spf13/cobra"
)

// flagReaderWriter is the minimal interface the capture command needs to read
// and restore the api-trigger-with-config project feature flag. Injected by
// tests; production uses a real *project.Client.
type flagReaderWriter interface {
	GetV11ProjectFeatureFlags(slug string) (map[string]bool, error)
	SetV11ProjectFeatureFlags(slug string, flags map[string]bool) error
}

// storageRetentionManager reads and writes org-level storage-retention controls
// before the extraction pipeline runs. Injected by tests; production uses a
// real *org.Client which satisfies this interface directly.
type storageRetentionManager interface {
	GetStorageRetention(orgUUID string) (*org.StorageRetention, error)
	SetStorageRetention(orgUUID string, controls org.StorageRetentionControls) error
}

// orgFlagManager reads and writes org-level feature flags via the v1.1
// organization settings endpoint.  Injected by tests; production uses a real
// *org.Client.
type orgFlagManager interface {
	GetFeatureFlags(vcsType, orgName string) (map[string]bool, error)
	UpdateFeatureFlags(vcsType, orgName string, flags map[string]bool) error
}

// pipelineDefLister lists pipeline definitions for a project so the command
// can resolve the first definition's UUID automatically.
type pipelineDefLister interface {
	ListPipelineDefinitions(projectID string) ([]project.PipelineDefinition, error)
}

// projectGetter retrieves project metadata (used to get the project UUID).
type projectGetter interface {
	GetProject(slug string) (*project.Project, error)
}

// contextRestrictionManager manages context restrictions during capture: it can
// list the live restrictions (to get their IDs for deletion) and create or
// delete individual restrictions.  Injected by tests; production uses a real
// *apicontext.Client.
type contextRestrictionManager interface {
	ListRestrictions(contextID string) ([]apicontext.Restriction, error)
	CreateRestriction(contextID, restrictionType, restrictionValue string) error
	DeleteRestriction(contextID, restrictionID string) error
}

// captureClient combines all interfaces the capture command exercises.
type captureClient interface {
	flagReaderWriter
	pipelineDefLister
	projectGetter
	contextRestrictionManager
	extract.Deps
}

const orgApiTriggerKey = "allow_api_trigger_with_config"

// errSkipProject is a sentinel that captureProject returns when a project is
// deliberately skipped (e.g. no pipeline definitions).  The outer loop treats
// this as informational rather than a hard error so capture continues for the
// remaining projects.
var errSkipProject = fmt.Errorf("project skipped")

// parseOrgSlug converts a manifest org slug into the (vcsType, orgName) pair
// expected by the v1.1 org-settings endpoint.
//
//   - "gh/<org>"        → ("github", "<org>")
//   - "bb/<org>"        → ("bitbucket", "<org>")
//   - "circleci/<uuid>" → ("circleci", "<uuid>")
//
// ok is false when the slug is empty or malformed.
func parseOrgSlug(slug string) (vcsType, orgName string, ok bool) {
	parts := strings.SplitN(slug, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	switch parts[0] {
	case "gh", "github":
		return "github", parts[1], true
	case "bb", "bitbucket":
		return "bitbucket", parts[1], true
	case "circleci":
		return "circleci", parts[1], true
	default:
		return parts[0], parts[1], true
	}
}

// isDefaultAllMembersGroup reports whether r is the default "All members"
// group restriction (type=="group", value==orgID).  Every App-org context has
// this restriction automatically; it is NOT a real access restriction.
func isDefaultAllMembersGroup(r manifest.Restriction, orgID string) bool {
	return r.Type == "group" && r.Value == orgID
}

// isGroupRestriction reports whether r is any group restriction
// (type=="group").  Group restrictions are ONLY supported on GitHub OAuth
// ("gh/…") orgs; they cannot be created via the API on standalone
// ("circleci/…") or Bitbucket orgs.  The capture command therefore NEVER
// removes or recreates group restrictions: attempting to do so on a
// non-OAuth org would fail with "This is only supported for OAuth orgs."
// Only `project` and `expression` restrictions are touched during capture.
func isGroupRestriction(r manifest.Restriction) bool {
	return r.Type == "group"
}

// realRestrictions filters out the default "All members" group restriction
// (type=="group" with value==orgID) from the supplied list.  Every App-org
// context has this restriction by default; it is NOT a real restriction
// — it simply means "all org members".  A context is considered genuinely
// restricted only when at least one non-All-members restriction remains.
//
// NOTE: non-default group restrictions (type=="group", value!=orgID) ARE real
// restrictions and remain in the list so callers can warn about them.
// The remove/restore path in prepareRestrictionRemoval explicitly skips all
// group restrictions (including non-default ones) because they are org-type
// specific: they can only be created on GitHub OAuth orgs, not standalone or
// Bitbucket orgs.  Users are directed to re-apply them manually.
func realRestrictions(restrictions []manifest.Restriction, orgID string) []manifest.Restriction {
	out := make([]manifest.Restriction, 0, len(restrictions))
	for _, r := range restrictions {
		if isDefaultAllMembersGroup(r, orgID) {
			// Default "All members" restriction — skip it.
			continue
		}
		out = append(out, r)
	}
	return out
}

const apiTriggerKey = "api-trigger-with-config"

// captureEncryptOpts groups the encryption-related flags for 'secrets capture'.
// They are resolved from flags by newSecretsCaptureCommand and threaded through
// to captureProject / CaptureWithDecrypt.
//
// NOTE: fields that need to be inspectable by CaptureWalkthroughResult (which
// is exported for tests) are also exported here.
type captureEncryptOpts struct {
	encrypt       bool   // Encrypt (exported via EncryptEnabled) — encrypt the artifact
	sshPublicKey  string // path to SSH public key file (recipient)
	sshPrivateKey string // path to SSH private key file (identity for local decrypt)
	generateKey   bool   // generate a fresh X25519 keypair and use it
	recipientStr  string // resolved public key string (after reading file / generating)
	identityFile  string // resolved private key/identity file path
	storage       string // "artifact" | "s3" | "both"
	s3Bucket      string
	s3Prefix      string
}

// EncryptEnabled reports whether encryption is enabled.
func (o captureEncryptOpts) EncryptEnabled() bool { return o.encrypt }

// GenerateKey reports whether a fresh keypair should be generated.
func (o captureEncryptOpts) GenerateKey() bool { return o.generateKey }

// SSHPublicKey returns the path to the SSH public key file.
func (o captureEncryptOpts) SSHPublicKey() string { return o.sshPublicKey }

// SSHPrivateKey returns the path to the SSH private key file.
func (o captureEncryptOpts) SSHPrivateKey() string { return o.sshPrivateKey }

// Storage returns the storage mode string.
func (o captureEncryptOpts) Storage() string { return o.storage }

// S3Bucket returns the S3 bucket name.
func (o captureEncryptOpts) S3Bucket() string { return o.s3Bucket }

// S3Prefix returns the S3 key prefix.
func (o captureEncryptOpts) S3Prefix() string { return o.s3Prefix }

// CaptureWalkthroughResult holds all the values collected by the interactive
// guided walkthrough for 'secrets capture'.  Fields are exported so that
// external test packages (cmd_test) can construct and inspect the result.
type CaptureWalkthroughResult struct {
	ManifestPath          string
	Output                string
	ProjectSlugs          []string
	ContextNames          []string
	HostProjectSlug       string // project used to run CONTEXT extraction
	Branch                string
	EnableTrigger         bool
	ArtifactRetentionDays int
	EncOpts               captureEncryptOpts
}

// runCaptureWalkthrough launches the interactive guided walkthrough for the
// capture command. It writes prompts to cmd.ErrOrStderr() and reads answers
// from os.Stdin.  The function delegates to RunCaptureWalkthroughWith so that
// tests can inject synthetic I/O via NewPrompter.
func runCaptureWalkthrough(
	cmd *cobra.Command,
	initial CaptureWalkthroughResult,
) (CaptureWalkthroughResult, error) {
	return RunCaptureWalkthroughWith(
		NewPrompter(os.Stdin, cmd.ErrOrStderr()),
		cmd,
		initial,
	)
}

// RunCaptureWalkthroughWith is the injectable interactive walkthrough used by
// both the command (via runCaptureWalkthrough) and external test files.
// p supplies the I/O streams; cmd is used for printing the confirmation summary.
func RunCaptureWalkthroughWith(
	p *Prompter,
	cmd *cobra.Command,
	initial CaptureWalkthroughResult,
) (CaptureWalkthroughResult, error) {
	out := p.out
	res := initial

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "╔══════════════════════════════════════════════════════════╗")
	fmt.Fprintln(out, "║  CircleCI Secret Capture — guided mode (RECOMMENDED)     ║")
	fmt.Fprintln(out, "╚══════════════════════════════════════════════════════════╝")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "This guided flow extracts secret values from CircleCI WITHOUT committing")
	fmt.Fprintln(out, "any config to your repositories. The CLI builds an inline pipeline config,")
	fmt.Fprintln(out, "triggers a run, and downloads the captured values automatically.")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Tip: re-run with all flags set to bypass these prompts (CI-safe).")
	fmt.Fprintln(out, "")

	// ── Step 1: Manifest path ─────────────────────────────────────────────────
	fmt.Fprintln(out, "Step 1 of 8 — Manifest")
	fmt.Fprintln(out, "  The manifest records variable names for all contexts and projects.")
	fmt.Fprintln(out, "")

	var err error
	if res.ManifestPath == "" {
		res.ManifestPath, err = p.askWithDefault("Manifest file path", "manifest.json")
		if err != nil {
			return res, err
		}
		if res.ManifestPath == "" {
			res.ManifestPath = "manifest.json"
		}
	} else {
		fmt.Fprintf(out, "  Manifest: %s  (from --manifest)\n", res.ManifestPath)
	}

	// Load the manifest so we can offer context/project lists.
	clog.Infof("capture walkthrough: loading manifest from %s", res.ManifestPath)
	m, loadErr := manifest.Load(res.ManifestPath)
	if loadErr != nil {
		return res, fmt.Errorf("loading manifest %s: %w", res.ManifestPath, loadErr)
	}

	// ── Step 2: What to extract ───────────────────────────────────────────────
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Step 2 of 8 — What to extract")
	fmt.Fprintln(out, "  Select the contexts and/or projects to capture.")
	fmt.Fprintln(out, "")

	// Collect context names from manifest.
	ctxOptions := make([]string, 0, len(m.Contexts))
	for _, mc := range m.Contexts {
		ctxOptions = append(ctxOptions, mc.Name)
	}

	// Collect project slugs from manifest. Also build display labels (Name + slug).
	// projSlugByLabel maps display label → slug so we can convert user selections
	// back to slugs after the multi-select prompt.
	projOptions := make([]string, 0, len(m.Projects))           // display labels
	projSlugByLabel := make(map[string]string, len(m.Projects)) // label → slug

	// Bug 3: default to only projects that have secrets (>=1 env var).
	projWithSecrets := make([]string, 0, len(m.Projects))
	projWithoutSecrets := 0
	for _, mp := range m.Projects {
		label := mp.Slug
		if mp.Name != "" && mp.Name != mp.Slug {
			label = fmt.Sprintf("%s (%s)", mp.Name, mp.Slug)
		}
		projOptions = append(projOptions, label)
		projSlugByLabel[label] = mp.Slug
		if len(mp.EnvVars) > 0 {
			projWithSecrets = append(projWithSecrets, label)
		} else {
			projWithoutSecrets++
		}
	}

	// Helper: convert display labels back to slugs.
	labelsToSlugs := func(labels []string) []string {
		slugs := make([]string, 0, len(labels))
		for _, lbl := range labels {
			if s, ok := projSlugByLabel[lbl]; ok {
				slugs = append(slugs, s)
			} else {
				// Fallback: treat the label itself as a slug (for backward compat).
				slugs = append(slugs, lbl)
			}
		}
		return slugs
	}

	// Only prompt if not already supplied via flags.
	if len(res.ContextNames) == 0 && len(ctxOptions) > 0 {
		chosen, selErr := p.askMultiSelect(
			fmt.Sprintf("Select contexts to capture (%d in manifest, default: all):", len(ctxOptions)),
			ctxOptions,
		)
		if selErr != nil {
			return res, selErr
		}
		res.ContextNames = chosen
	} else if len(ctxOptions) == 0 {
		fmt.Fprintln(out, "  (no contexts found in manifest)")
	} else {
		fmt.Fprintf(out, "  Contexts: %v  (from --context)\n", res.ContextNames)
	}

	if len(res.ProjectSlugs) == 0 && len(projOptions) > 0 {
		// Bug 3: default selection to projects-with-secrets only.
		defaultLabels := projWithSecrets
		if len(defaultLabels) == 0 {
			defaultLabels = projOptions // no projects have secrets → show all
		}
		promptMsg := fmt.Sprintf("Select projects to capture project env vars for (%d in manifest, default: %d with secrets):", len(projOptions), len(projWithSecrets))
		if projWithoutSecrets > 0 {
			fmt.Fprintf(out, "  NOTE: %d project(s) have no env vars in the manifest and are hidden from defaults.\n", projWithoutSecrets)
		}
		chosen, selErr := p.askMultiSelectWithDefault(
			promptMsg,
			projOptions,
			defaultLabels,
		)
		if selErr != nil {
			return res, selErr
		}
		res.ProjectSlugs = labelsToSlugs(chosen)
	} else if len(projOptions) == 0 {
		fmt.Fprintln(out, "  (no projects found in manifest)")
	} else {
		fmt.Fprintf(out, "  Projects: %v  (from --project)\n", res.ProjectSlugs)
	}

	// ── Step 3: Host project for context extraction ───────────────────────────
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Step 3 of 8 — Host project for CONTEXT extraction")
	fmt.Fprintln(out, "  Context env vars are extracted by attaching the context to a pipeline run.")
	fmt.Fprintln(out, "  You must choose which project's pipeline to run this under.")
	fmt.Fprintln(out, "  (Any project works — build history doesn't matter, only the extraction does.)")
	fmt.Fprintln(out, "")

	hasContexts := len(res.ContextNames) > 0
	if len(res.ContextNames) == 0 && len(ctxOptions) == 0 {
		hasContexts = false
	}
	// If contextNames is empty slice (user selected none), skip host project.
	if hasContexts && len(res.ContextNames) == 0 {
		hasContexts = false
	}

	if hasContexts {
		if res.HostProjectSlug == "" {
			// Build project display labels for host selection.
			// Prefer to auto-pick the first project if only one available.
			if len(projOptions) == 1 {
				firstSlug := projSlugByLabel[projOptions[0]]
				if firstSlug == "" {
					firstSlug = projOptions[0]
				}
				fmt.Fprintf(out, "  Only one project in manifest — auto-selecting %s as the host project.\n", projOptions[0])
				res.HostProjectSlug = firstSlug
			} else if len(projOptions) == 0 {
				return res, fmt.Errorf("cannot extract contexts: no projects found in manifest (a host project is required to run the extraction pipeline under)")
			} else {
				// Let user choose, or auto-pick first.
				firstSlug := projSlugByLabel[projOptions[0]]
				if firstSlug == "" {
					firstSlug = projOptions[0]
				}
				autoPickOption := "(auto-pick first: " + projOptions[0] + ")"
				hostOptions := append([]string{autoPickOption}, projOptions...)
				chosen, choiceErr := p.askChoice(
					"Choose host project to run the CONTEXT extraction pipeline under:",
					hostOptions,
				)
				if choiceErr != nil {
					return res, choiceErr
				}
				if chosen == autoPickOption {
					res.HostProjectSlug = firstSlug
					fmt.Fprintf(out, "  Host project: %s\n", res.HostProjectSlug)
				} else {
					// Convert label back to slug.
					if s, ok := projSlugByLabel[chosen]; ok {
						res.HostProjectSlug = s
					} else {
						res.HostProjectSlug = chosen
					}
				}
			}
		} else {
			fmt.Fprintf(out, "  Host project: %s  (from --host-project)\n", res.HostProjectSlug)
		}
	} else {
		fmt.Fprintln(out, "  No contexts selected — skipping host project selection.")
	}

	// ── Step 4: Encryption ────────────────────────────────────────────────────
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Step 4 of 8 — Encryption (RECOMMENDED)")
	fmt.Fprintln(out, "  When enabled, the in-pipeline artifact is age-encrypted.")
	fmt.Fprintln(out, "  Plaintext secrets NEVER persist in CircleCI artifact storage.")
	fmt.Fprintln(out, "")

	if !res.EncOpts.encrypt && res.EncOpts.sshPublicKey == "" && !res.EncOpts.generateKey {
		doEncrypt, encErr := p.askBool("Encrypt the captured secrets?", true)
		if encErr != nil {
			return res, encErr
		}
		res.EncOpts.encrypt = doEncrypt

		if doEncrypt {
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, "  Encryption key options:")
			fmt.Fprintln(out, "    1) Generate a fresh keypair automatically (recommended, no key required)")
			fmt.Fprintln(out, "    2) Use an existing SSH public key file")
			fmt.Fprintln(out, "")
			keyChoice, keyErr := p.askChoice(
				"How to provide the encryption key?",
				[]string{"generate a fresh keypair (--generate-key)", "use existing SSH public key (--ssh-public-key)"},
			)
			if keyErr != nil {
				return res, keyErr
			}
			if strings.HasPrefix(keyChoice, "generate") {
				res.EncOpts.generateKey = true
				fmt.Fprintln(out, "  A fresh age X25519 keypair will be generated for this run.")
				fmt.Fprintln(out, "  Keep the identity file (migration-identity.age) — you will need it to decrypt.")
			} else {
				pubKeyPath, pkErr := p.askRequired("Path to SSH public key file", "e.g. ~/.ssh/id_ed25519.pub")
				if pkErr != nil {
					return res, pkErr
				}
				res.EncOpts.sshPublicKey = pubKeyPath

				privKeyPath, privErr := p.askWithDefault("Path to SSH private key for local decryption", "~/.ssh/id_ed25519")
				if privErr != nil {
					return res, privErr
				}
				res.EncOpts.sshPrivateKey = privKeyPath
			}
		} else {
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, "  WARNING: captured secrets will be PLAINTEXT in the CircleCI artifact.")
			fmt.Fprintln(out, "  Build artifacts are retained for at least 1 day with no delete API.")
			fmt.Fprintln(out, "  Encryption is strongly recommended for production secrets.")
		}
	} else {
		if res.EncOpts.encrypt {
			fmt.Fprintln(out, "  Encryption: enabled  (from --encrypt)")
		} else {
			fmt.Fprintln(out, "  Encryption: disabled  (from flags)")
		}
	}

	// ── Step 5: Storage ───────────────────────────────────────────────────────
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Step 5 of 8 — Storage")
	fmt.Fprintln(out, "  Where to store the (optionally encrypted) bundle after extraction.")
	fmt.Fprintln(out, "")

	if res.EncOpts.storage == "" {
		storageChoice, storErr := p.askChoice(
			"Storage mode for the extracted bundle:",
			[]string{"artifact (default — CircleCI job artifact)", "s3 (S3 upload; requires AWS creds in job)", "both (artifact + S3)"},
		)
		if storErr != nil {
			return res, storErr
		}
		switch {
		case strings.HasPrefix(storageChoice, "s3"):
			res.EncOpts.storage = "s3"
		case strings.HasPrefix(storageChoice, "both"):
			res.EncOpts.storage = "both"
		default:
			res.EncOpts.storage = "artifact"
		}

		if res.EncOpts.storage == "s3" || res.EncOpts.storage == "both" {
			bucket, bErr := p.askRequired("S3 bucket name", "--s3-bucket")
			if bErr != nil {
				return res, bErr
			}
			res.EncOpts.s3Bucket = bucket

			prefix, pErr := p.askWithDefault("S3 key prefix (optional)", "migration/")
			if pErr != nil {
				return res, pErr
			}
			res.EncOpts.s3Prefix = prefix

			fmt.Fprintln(out, "  NOTE: the job must have AWS credentials via a context or project env vars.")
		}
	} else {
		fmt.Fprintf(out, "  Storage: %s  (from --storage)\n", res.EncOpts.storage)
	}

	// ── Step 6: Artifact retention ────────────────────────────────────────────
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Step 6 of 8 — Artifact retention (security)")
	fmt.Fprintln(out, "  Setting retention to 1 day minimises how long secrets linger in artifacts.")
	fmt.Fprintln(out, "  NOTE: this lowers the ENTIRE ORG's artifact retention, not just this job.")
	fmt.Fprintln(out, "  The prior value is logged so you can restore it manually afterwards.")
	fmt.Fprintln(out, "")

	if res.ArtifactRetentionDays == 0 {
		setRetention, retErr := p.askBool("Set artifact retention to 1 day (recommended minimum)?", true)
		if retErr != nil {
			return res, retErr
		}
		if setRetention {
			res.ArtifactRetentionDays = 1
		}
	} else {
		fmt.Fprintf(out, "  Artifact retention: %d day(s)  (from --artifact-retention-days)\n", res.ArtifactRetentionDays)
	}

	// ── Step 7: Output path and branch ───────────────────────────────────────
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Step 7 of 8 — Output path and branch")
	fmt.Fprintln(out, "  Choose where to write the local secrets bundle and which branch to run on.")
	fmt.Fprintln(out, "")

	if res.Output == "" {
		outputVal, outErr := p.askWithDefault("Output bundle path", "secrets.json")
		if outErr != nil {
			return res, outErr
		}
		if outputVal == "" {
			outputVal = "secrets.json"
		}
		res.Output = outputVal
	} else {
		fmt.Fprintf(out, "  Output bundle: %s  (from --output)\n", res.Output)
	}

	if res.Branch == "" {
		branchVal, brErr := p.askWithDefault("Branch for the extraction run", "main")
		if brErr != nil {
			return res, brErr
		}
		if branchVal == "" {
			branchVal = "main"
		}
		res.Branch = branchVal
	} else {
		fmt.Fprintf(out, "  Branch: %s  (from --branch)\n", res.Branch)
	}

	// ── Step 8: Enable trigger ────────────────────────────────────────────────
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Step 8 of 8 — Enable api-trigger-with-config")
	fmt.Fprintln(out, "  The extraction pipeline uses an inline (unversioned) config trigger.")
	fmt.Fprintln(out, "  If the project does not have api-trigger-with-config enabled, the CLI can")
	fmt.Fprintln(out, "  enable it temporarily and restore the original setting after capture.")
	fmt.Fprintln(out, "")

	if !res.EnableTrigger {
		doEnable, enErr := p.askBool(
			"Enable api-trigger-with-config automatically if needed (and restore after)?",
			true,
		)
		if enErr != nil {
			return res, enErr
		}
		res.EnableTrigger = doEnable
	} else {
		fmt.Fprintln(out, "  Enable trigger: yes  (from --enable-trigger)")
	}

	// ── Confirmation summary ──────────────────────────────────────────────────
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "──────────────────────────────────────────────────────────────")
	fmt.Fprintln(out, "SUMMARY — review before running:")
	fmt.Fprintf(out, "  Manifest:            %s\n", res.ManifestPath)
	fmt.Fprintf(out, "  Output bundle:       %s\n", res.Output)
	if len(res.ContextNames) > 0 {
		fmt.Fprintf(out, "  Contexts:            %v\n", res.ContextNames)
		fmt.Fprintf(out, "  Host project:        %s\n", res.HostProjectSlug)
	}
	if len(res.ProjectSlugs) > 0 {
		fmt.Fprintf(out, "  Projects:            %v\n", res.ProjectSlugs)
	}
	if res.EncOpts.encrypt {
		if res.EncOpts.generateKey {
			fmt.Fprintln(out, "  Encryption:          age (generate-key)")
		} else {
			fmt.Fprintf(out, "  Encryption:          age (public key: %s)\n", res.EncOpts.sshPublicKey)
		}
	} else {
		fmt.Fprintln(out, "  Encryption:          NONE (plaintext artifact)")
	}
	fmt.Fprintf(out, "  Storage:             %s\n", func() string {
		if res.EncOpts.storage == "" {
			return "artifact"
		}
		return res.EncOpts.storage
	}())
	fmt.Fprintf(out, "  Branch:              %s\n", res.Branch)
	fmt.Fprintf(out, "  Enable trigger:      %v\n", res.EnableTrigger)
	if res.ArtifactRetentionDays > 0 {
		fmt.Fprintf(out, "  Artifact retention:  %d day(s)\n", res.ArtifactRetentionDays)
	}
	fmt.Fprintln(out, "──────────────────────────────────────────────────────────────")
	fmt.Fprintln(out, "")

	confirmed, confErr := p.askBool("Proceed with capture?", true)
	if confErr != nil {
		return res, confErr
	}
	if !confirmed {
		return res, fmt.Errorf("capture cancelled by user")
	}

	return res, nil
}

// newSecretsCaptureCommand builds the "secrets capture" subcommand.
func newSecretsCaptureCommand() *cobra.Command {
	var (
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
		noEncrypt             bool // explicit opt-out; overrides the encrypt=true default
		pollTimeout           time.Duration
		artifactRetentionDays int
		encOpts               captureEncryptOpts
		sshKeysCapture        bool // when true, extract SSH private keys for projects with cataloged keys
	)

	cmd := &cobra.Command{
		Use:   "capture [--manifest <file>]",
		Short: "Capture secret values by running an unversioned pipeline inside CircleCI (RECOMMENDED).",
		Long: `capture is the RECOMMENDED way to extract secret values from CircleCI.

It extracts plaintext environment-variable values WITHOUT committing any config
to the target repository. The CLI builds an inline (unversioned) pipeline config,
triggers a run inside CircleCI, and downloads the captured values automatically.

  RECOMMENDED: run 'secrets capture' on an interactive terminal without flags to
  launch the guided walkthrough. It prompts for each option with sensible defaults
  and explicit guidance on host-project selection for context extraction.

NOTE: interactive prompts are written to stderr; if you pipe stdout while
relying on the guided prompts, use a TTY for stdin — piping stdin triggers
non-TTY mode and all flags must be supplied explicitly.

  For the orb-based alternative (committed config), see:
    circleci-migrate orb inline --help
    circleci-migrate secrets extract --help

HOW IT WORKS:
  1. Reads variable names from the manifest for the selected project(s) and
     context(s).
  2. Ensures api-trigger-with-config is enabled for each project (either it
     must already be on, or --enable-trigger must be set).
  3. Triggers an unversioned pipeline run with an inline config that dumps the
     variable values to a build artifact.
  4. Polls until the pipeline completes, then downloads and parses the artifact.
  5. Writes the captured values into the secret bundle (--output).
  6. Restores the api-trigger-with-config flag to its original value (even on
     failure).

HOST PROJECT FOR CONTEXT EXTRACTION:
  Context env vars are injected into a job that references the context.
  The pipeline must run under some project — this is the "host project".
  Any project works; build history is irrelevant (only extraction matters).
  Use --host-project to specify it; the guided mode prompts you to choose.
  Project env vars are always captured under each project's own pipeline.

ENCRYPTION (default: ON — use --no-encrypt to opt out):
  By default, the in-pipeline extraction job encrypts the artifact with age so
  that plaintext secrets NEVER persist in CircleCI artifact storage. Encryption
  requires a public key: supply --ssh-public-key or --generate-key. When neither
  is given, capture auto-generates a fresh keypair (--generate-key behaviour).

  After the run, capture downloads the .age artifact and decrypts it locally
  with --ssh-private-key (or the generated key) to build the in-memory bundle.

  Use --no-encrypt to disable encryption and accept a PLAINTEXT artifact. This
  is strongly discouraged for production secrets — build artifacts are retained
  for at least 1 day and there is no delete-artifact API.

  Use --generate-key to have capture create a fresh age X25519 keypair
  automatically, print the file paths, and use it for this run.

STORAGE (--storage):
  artifact (default) — store the bundle as a CircleCI job artifact.
  s3                 — upload to S3 only (requires aws CLI + AWS creds in job).
  both               — store in both artifact and S3.

  For S3 storage provide --s3-bucket and (optionally) --s3-prefix.
  The job executor must have AWS credentials via a context or project env vars.

SECURITY NOTES:
  - Without --encrypt: the secret bundle contains plaintext secrets. Protect it.
  - Build artifacts are retained for at least 1 day; there is no delete API.
    With --encrypt the artifact is age-encrypted so plaintext never hits disk.
  - Rotate any captured secrets after migration.

Examples:
  # Interactive guided walkthrough (recommended for first-time use):
  circleci-migrate secrets capture

  # Non-interactive with encryption (default; auto-generates a keypair):
  circleci-migrate secrets capture --manifest manifest.json --source-token $TOKEN
  circleci-migrate secrets capture --manifest manifest.json --project gh/acme/web \
    --enable-trigger --branch main -o secrets.json
  # Encrypted capture with auto-generated key (explicit):
  circleci-migrate secrets capture --manifest manifest.json --generate-key
  # Encrypted capture with existing SSH key:
  circleci-migrate secrets capture --manifest manifest.json \
    --ssh-public-key ~/.ssh/id_ed25519.pub --ssh-private-key ~/.ssh/id_ed25519
  # Opt out of encryption (PLAINTEXT artifact — NOT recommended):
  circleci-migrate secrets capture --manifest manifest.json --no-encrypt
  # Context capture specifying host project explicitly:
  circleci-migrate secrets capture --manifest manifest.json \
    --context deploy-prod --host-project gh/acme/web --enable-trigger
  # Upload encrypted bundle to S3 instead of artifact:
  circleci-migrate secrets capture --manifest manifest.json --generate-key \
    --storage s3 --s3-bucket my-migration-bucket --s3-prefix migration/`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// ── Interactive guided mode ────────────────────────────────────────
			// Fire when on a TTY and not --no-input AND not enough flags to run
			// non-interactively (manifest path is the minimum required flag).
			missingManifest := manifestPath == ""
			wantsInteraction := missingManifest && !noInput

			if wantsInteraction && !isInteractiveTTY() {
				// Non-TTY with missing required flags: fail clearly.
				return fmt.Errorf("--manifest is required in non-interactive mode; " +
					"run 'secrets capture' on an interactive terminal for the guided walkthrough")
			}

			if wantsInteraction {
				// Launch interactive walkthrough.
				// For storage: only pre-fill if the user explicitly changed the flag
				// (otherwise the default "artifact" would suppress the storage prompt).
				walkthroughStorage := ""
				if cmd.Flags().Changed("storage") {
					walkthroughStorage = encOpts.storage
				}
				// For encrypt: reset to zero so the walkthrough always asks.
				// The flag defaults to true but we never want to suppress step 4.
				walkthroughEncOpts := encOpts
				walkthroughEncOpts.storage = walkthroughStorage
				walkthroughEncOpts.encrypt = false // walkthrough asks; don't pre-fill

				// For branch, output, and enable-trigger: only pre-fill if user
				// explicitly set them (otherwise the default values would suppress
				// the walkthrough prompts for those options).
				walkthroughBranch := ""
				if cmd.Flags().Changed("branch") {
					walkthroughBranch = branch
				}
				walkthroughEnableTrigger := false
				if cmd.Flags().Changed("enable-trigger") {
					walkthroughEnableTrigger = enableTrigger
				}
				walkthroughOutput := ""
				if cmd.Flags().Changed("output") {
					walkthroughOutput = output
				}

				initial := CaptureWalkthroughResult{
					ManifestPath:          manifestPath,
					Output:                walkthroughOutput,
					ProjectSlugs:          projectSlugs,
					ContextNames:          contextNames,
					HostProjectSlug:       hostProjectSlug,
					Branch:                walkthroughBranch,
					EnableTrigger:         walkthroughEnableTrigger,
					ArtifactRetentionDays: artifactRetentionDays,
					EncOpts:               walkthroughEncOpts,
				}
				result, wErr := runCaptureWalkthrough(cmd, initial)
				if wErr != nil {
					return wErr
				}
				// Apply walkthrough results back to local vars.
				manifestPath = result.ManifestPath
				output = result.Output
				projectSlugs = result.ProjectSlugs
				contextNames = result.ContextNames
				hostProjectSlug = result.HostProjectSlug
				if result.Branch != "" {
					branch = result.Branch
				}
				enableTrigger = result.EnableTrigger
				artifactRetentionDays = result.ArtifactRetentionDays
				// Merge encOpts: walkthrough may have set storage, encrypt, etc.
				if result.EncOpts.storage != "" {
					encOpts.storage = result.EncOpts.storage
				}
				// Always take the walkthrough's encrypt decision (it asked the user).
				encOpts.encrypt = result.EncOpts.encrypt
				if result.EncOpts.encrypt {
					encOpts.generateKey = result.EncOpts.generateKey
					encOpts.sshPublicKey = result.EncOpts.sshPublicKey
					encOpts.sshPrivateKey = result.EncOpts.sshPrivateKey
				}
				if result.EncOpts.s3Bucket != "" {
					encOpts.s3Bucket = result.EncOpts.s3Bucket
				}
				if result.EncOpts.s3Prefix != "" {
					encOpts.s3Prefix = result.EncOpts.s3Prefix
				}
			}

			if manifestPath == "" {
				return errors.New("--manifest is required")
			}

			// ── Resolve encrypt/no-encrypt after walkthrough ─────────────────
			// In non-interactive mode (wantsInteraction==false), apply the
			// --no-encrypt override and handle the "no explicit choice" case.
			if !wantsInteraction {
				encryptChanged := cmd.Flags().Changed("encrypt")
				noEncryptChanged := cmd.Flags().Changed("no-encrypt")
				generateKeyChanged := cmd.Flags().Changed("generate-key")
				sshPubKeyChanged := cmd.Flags().Changed("ssh-public-key")

				// In --no-input mode every unattended run must carry an explicit
				// encryption decision so there are no silent plaintext captures.
				// Any of these flags count as an explicit choice:
				//   --encrypt, --no-encrypt, --generate-key, --ssh-public-key
				hasExplicitChoice := encryptChanged || noEncryptChanged ||
					generateKeyChanged || sshPubKeyChanged
				if noInput && !hasExplicitChoice {
					return fmt.Errorf(
						"--no-input requires an explicit encryption choice: " +
							"use --generate-key (encrypt with auto-generated key), " +
							"--ssh-public-key <path> (encrypt with existing key), " +
							"or --no-encrypt (opt out — NOT recommended for production secrets)")
				}

				if noEncryptChanged {
					// --no-encrypt was explicitly passed; override the default.
					encOpts.encrypt = !noEncrypt
				}
				// else: encOpts.encrypt retains its default (true) or the value
				// set by --encrypt if the user passed that flag explicitly.
			}

			// When encrypt is enabled and no key material supplied, auto-generate.
			// This matches the walkthrough's "generate-key" default and allows
			// zero-argument non-interactive use: `secrets capture --manifest m.json`.
			if encOpts.encrypt && !encOpts.generateKey && encOpts.sshPublicKey == "" {
				encOpts.generateKey = true
				clog.Infof("capture: no key supplied with --encrypt; auto-enabling --generate-key")
			}

			token := rootOptions.SourceTokenOrDefault()
			if token == "" {
				return fmt.Errorf("no API token: set --source-token, --token, CIRCLECI_SOURCE_TOKEN, or CIRCLECI_CLI_TOKEN")
			}

			// ── Resolve encryption options ────────────────────────────────────
			if encOpts.encrypt {
				if err := resolveEncryptOpts(cmd, &encOpts); err != nil {
					return err
				}
			}

			// ── Validate storage flags ────────────────────────────────────────
			if err := validateStorageFlags(&encOpts); err != nil {
				return err
			}

			m, err := manifest.Load(manifestPath)
			if err != nil {
				return err
			}

			bndl, err := loadOrNewBundle(output)
			if err != nil {
				return err
			}

			projClient, err := project.NewClient(rootOptions, token)
			if err != nil {
				return fmt.Errorf("creating project client: %w", err)
			}

			ctxClient, err := newContextClientForCapture(rootOptions, token)
			if err != nil {
				return fmt.Errorf("creating context client: %w", err)
			}

			capClient := &combinedCaptureClient{
				flagReaderWriter:          projClient,
				pipelineDefLister:         projClient,
				projectGetter:             projClient,
				contextRestrictionManager: ctxClient,
				Deps:                      projClient,
			}

			// ── Scoping (Fix 2: context/project separation) ───────────────────
			// Determine whether the caller explicitly chose contexts and/or projects.
			// explicitContexts: --context was provided → capture exactly those.
			// explicitProjects: --project was provided → capture exactly those.
			//
			// When neither flag is given both sets default to "with values":
			//   contexts → all that have ≥1 env var
			//   projects → all that have ≥1 env var (via selectProjects default)
			//
			// When --context is given but NOT --project, we ONLY run context
			// extraction under the host project; the per-project loop is skipped
			// entirely.  This prevents accidental full-org sweeps.
			explicitContexts := len(contextNames) > 0
			explicitProjects := len(projectSlugs) > 0

			// Resolve the set of projects to process for project-env extraction.
			// - Explicit: use the given slugs.
			// - Default: projects with ≥1 env var (selectProjects no-slug path).
			// - If --context given without --project: skip project loop entirely.
			var projects []manifest.Project
			runProjectLoop := true
			if explicitContexts && !explicitProjects {
				// Context-only request: do not run per-project env-var extraction.
				runProjectLoop = false
			} else {
				projects = selectProjects(m, projectSlugs)
			}

			if len(projects) == 0 && hostProjectSlug == "" && !explicitContexts {
				return fmt.Errorf("no projects matched the given selectors (manifest has %d projects)", len(m.Projects))
			}

			// Pre-resolve the set of context names the caller wants to include.
			// Empty selectedCtxNames means "include all contexts with values".
			selectedCtxNames := make(map[string]bool, len(contextNames))
			for _, n := range contextNames {
				selectedCtxNames[n] = true
			}

			// When no --context flag was given, default to contexts-with-values.
			// Build a filter from the manifest itself.
			if !explicitContexts {
				for i := range m.Contexts {
					if len(m.Contexts[i].EnvVars) > 0 {
						selectedCtxNames[m.Contexts[i].Name] = true
					}
				}
			}

			// ── Org-level allow_api_trigger_with_config ───────────────────────
			// The pipeline/run endpoint also requires the ORG-level flag to be on.
			// We read-and-restore it ONCE before iterating projects (not per-project).
			// On any error we warn and continue — the per-project flag is the primary
			// gate and callers can fix the org flag manually.
			if enableTrigger {
				if vcsType, orgName, ok := parseOrgSlug(m.Source.Org.Slug); ok {
					orgClient, oerr := newOrgClientForCapture(rootOptions, token)
					if oerr != nil {
						fmt.Fprintf(cmd.ErrOrStderr(),
							"WARNING: could not create org client to check org-level flag: %v\n", oerr)
					} else {
						restoreOrgFlag := maybeEnableOrgTriggerFlag(cmd, orgClient, vcsType, orgName)
						defer restoreOrgFlag()
					}
				}
			}

			// ── Artifact-retention safety control ─────────────────────────────
			// When --artifact-retention-days is set, lower the artifact-retention
			// BEFORE triggering the extraction pipeline so that any secrets that
			// land in the build artifact expire quickly.  We deliberately do NOT
			// auto-restore: keeping artifact retention low is the safe default
			// when secrets may be present in artifacts.
			if artifactRetentionDays > 0 && m.Source.Org.ID != "" {
				orgRetentionClient, oerr := newOrgClientForCapture(rootOptions, token)
				if oerr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(),
						"WARNING: could not create org client for artifact-retention control: %v\n", oerr)
				} else {
					applyArtifactRetentionControl(cmd, orgRetentionClient, m.Source.Org.ID, artifactRetentionDays)
				}
			}

			var captureErr error

			// ── Bug 5: Build restriction decider ─────────────────────────────
			// In interactive mode (wantsInteraction was true earlier), ask the user
			// about restricted contexts instead of silently skipping them.
			// In non-interactive / --no-input mode, use nil (falls back to
			// skipRestricted / removeRestrictions flag logic).
			var resDecider restrictionDecider
			if wantsInteraction && isInteractiveTTY() {
				prompter := NewPrompter(os.Stdin, cmd.ErrOrStderr())
				resDecider = func(ctxName string, n int) (bool, error) {
					return prompter.askBool(
						fmt.Sprintf("Context %q has %d restriction(s) — temporarily remove them to extract (restored afterward)?", ctxName, n),
						false,
					)
				}
			}

			// ── Auto-pick host project when contexts need extraction ─────────────
			// If contexts are selected (by flag or by default-with-values) and no
			// --host-project was given, auto-pick:
			//   1. First project that has ≥1 pipeline definition (live check later).
			//   2. Fall back: first project in the manifest.
			// We record the auto-picked slug here and let captureProject do the live
			// definition check; if it returns errSkipProject we stop.
			hasContextsToCapture := len(selectedCtxNames) > 0
			if hasContextsToCapture && hostProjectSlug == "" {
				if len(m.Projects) == 0 {
					return fmt.Errorf("cannot extract contexts: no projects found in manifest (a host project is required)")
				}
				hostProjectSlug = m.Projects[0].Slug
				clog.Infof("capture: auto-picked host project %s for context extraction", hostProjectSlug)
				fmt.Fprintf(cmd.ErrOrStderr(), "Auto-picking host project %s for context extraction (use --host-project to override).\n", hostProjectSlug)
			}

			// ── Capture scope summary ─────────────────────────────────────────────
			// Print a one-line operator summary before starting any pipelines.
			{
				nCtx := len(selectedCtxNames)
				nProj := len(projects)
				fmt.Fprintf(cmd.ErrOrStderr(),
					"Capture scope: %d context(s) with values, %d project(s) with values, host project: %s\n",
					nCtx, nProj, func() string {
						if hostProjectSlug != "" {
							return hostProjectSlug
						}
						return "(none)"
					}(),
				)
			}

			// ── Context extraction via host project ───────────────────────────────
			// Context env vars are extracted ONCE under the host project — NOT in
			// each per-project run.  This is the key safety invariant: a context's
			// secrets are never dumped multiple times or under unintended projects.
			if hasContextsToCapture && hostProjectSlug != "" {
				hostProjManifest := findProjectBySlug(m, hostProjectSlug)
				if hostProjManifest == nil {
					// Host project not in manifest; synthesise a minimal entry so
					// captureProject can still look it up via the API.
					hostProjManifest = &manifest.Project{Slug: hostProjectSlug}
				}
				clog.Infof("capture: running CONTEXT extraction under host project %s", hostProjectSlug)
				fmt.Fprintf(cmd.ErrOrStderr(), "Running CONTEXT extraction under host project %s…\n", hostProjectSlug)
				// projectVarsOnly=false: capture contexts (and host project's own vars).
				// captureSSHKeys=false: SSH key extraction is only done in project-vars
				// mode (projectVarsOnly=true) so it runs under the correct project.
				if err := captureProject(
					cmd.Context(),
					cmd,
					capClient,
					m, bndl,
					hostProjManifest,
					selectedCtxNames,
					branch, output,
					enableTrigger, skipRestrictedCtxs, removeRestrictions,
					pollTimeout,
					encOpts,
					resDecider,
					false, // projectVarsOnly=false: include context extraction
					false, // captureSSHKeys=false: not in host-project context run
				); err != nil {
					if errors.Is(err, errSkipProject) {
						// Not a hard error; already printed a SKIP notice.
					} else {
						fmt.Fprintf(cmd.ErrOrStderr(), "ERROR capturing contexts under host project %s: %v\n", hostProjectSlug, err)
						if captureErr == nil {
							captureErr = err
						}
					}
				}
			}

			// ── Project env-var extraction (Fix 1) ───────────────────────────────
			// Each selected project's own env vars are captured under that project's
			// pipeline with projectVarsOnly=true.  This means the context loop is
			// SKIPPED entirely — contexts were already extracted under the host
			// project above.  An empty context filter is NOT enough here: even an
			// empty selectedCtxNames would still pass through the context loop and
			// could trigger unwanted pipeline attaches.
			if runProjectLoop {
				for i := range projects {
					p := &projects[i]
					if err := captureProject(
						cmd.Context(),
						cmd,
						capClient,
						m, bndl,
						p,
						nil, // selectedCtxNames unused when projectVarsOnly=true
						branch, output,
						enableTrigger, skipRestrictedCtxs, removeRestrictions,
						pollTimeout,
						encOpts,
						resDecider,
						true,           // projectVarsOnly=true: skip context loop entirely
						sshKeysCapture, // capture SSH private keys when flag is set
					); err != nil {
						if errors.Is(err, errSkipProject) {
							// Not a hard error; SKIP notice already printed in captureProject.
						} else {
							// Continue processing other projects; record the first error.
							fmt.Fprintf(cmd.ErrOrStderr(), "ERROR capturing project %s: %v\n", p.Slug, err)
							if captureErr == nil {
								captureErr = err
							}
						}
					}
				}
			}

			bndl.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
			bndl.ToolVersion = version.UserAgent()
			if err := bndl.Save(output); err != nil {
				return fmt.Errorf("writing secret bundle: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Secret bundle written to %s\n", output)

			if encOpts.encrypt {
				fmt.Fprintln(cmd.ErrOrStderr(), `
NOTE: Encryption was enabled. The CircleCI artifact was age-encrypted.
  Plaintext secrets never persisted in CircleCI artifact storage.
  The local bundle at `+output+` contains plaintext. Protect it.`)
			} else {
				fmt.Fprintln(cmd.ErrOrStderr(), `
WARNING: --no-encrypt was set. The secret bundle contains PLAINTEXT secrets.
  - Protect the file; do not commit it to version control.
  - Build artifacts from the extraction run are retained for at least 1 day.
    There is NO delete-artifact API. Rotate captured secrets and treat the
    artifact as sensitive until it expires.`)
			}

			return captureErr
		},
	}

	f := cmd.Flags()
	f.StringVar(&manifestPath, "manifest", "", "Path to the export manifest (prompted interactively when omitted on a TTY)")
	f.StringVarP(&output, "output", "o", "secrets.json", "Path to the secret bundle to write/append")
	f.StringArrayVar(&projectSlugs, "project", nil, "Project slug(s) to capture (default: all in manifest)")
	f.StringArrayVar(&contextNames, "context", nil, "Context name(s) to capture (default: all in manifest)")
	f.StringVar(&hostProjectSlug, "host-project", "",
		"Project slug to use when running the CONTEXT extraction pipeline. "+
			"Any project works — build history is irrelevant; only the extraction matters. "+
			"Prompted interactively when contexts are selected and this flag is absent.")
	f.StringVar(&branch, "branch", "main", "Branch to check out for the extraction run")
	f.BoolVar(&enableTrigger, "enable-trigger", false,
		"Enable api-trigger-with-config if not already on, and restore after capture")
	f.BoolVar(&skipRestrictedCtxs, "skip-restricted-contexts", true,
		"Skip contexts that have project/expression/group restrictions (attach warning instead of attempting)")
	f.BoolVar(&removeRestrictions, "remove-restrictions", false,
		"Temporarily remove real context restrictions before extraction and restore them afterwards (requires explicit opt-in)")
	f.BoolVar(&noInput, "no-input", false,
		"Disable all interactive prompts; error if a required value is missing (implied when stdin is not a TTY)")
	f.DurationVar(&pollTimeout, "poll-timeout", 10*time.Minute,
		"Maximum time to wait for each pipeline to complete (0 = no timeout)")
	f.IntVar(&artifactRetentionDays, "artifact-retention-days", 0,
		"Set the org's artifact-retention to this many days BEFORE triggering the extraction pipeline. "+
			"Recommended value: 1 (the minimum). Default 0 = leave unchanged. "+
			"The prior value is logged so you can restore it manually. "+
			"This control is NOT auto-restored after capture — keeping retention low is the safe default "+
			"when secrets may land in artifacts.")

	// ── Encryption flags ──────────────────────────────────────────────────────
	// Encryption is ON by default. --no-encrypt opts out; --encrypt is kept for
	// backward-compatibility (explicitly passing --encrypt is a no-op when
	// encryption is already the default).
	f.BoolVar(&encOpts.encrypt, "encrypt", true,
		"Encrypt the in-pipeline artifact with age so plaintext secrets never persist in CircleCI "+
			"(default: true). Supply --ssh-public-key or --generate-key; if neither is given a fresh "+
			"keypair is auto-generated. Use --no-encrypt to opt out.")
	f.BoolVar(&noEncrypt, "no-encrypt", false,
		"Disable artifact encryption and produce a PLAINTEXT secrets artifact. "+
			"NOT recommended for production secrets — build artifacts are retained for at least 1 day "+
			"and there is no delete-artifact API.")
	f.StringVar(&encOpts.sshPublicKey, "ssh-public-key", "",
		"Path to an SSH public key (.pub) or age recipients file used as the encryption recipient. "+
			"The public key is safe to embed in the pipeline config.")
	f.StringVar(&encOpts.sshPrivateKey, "ssh-private-key", "",
		"Path to an SSH private key or age identity file used to decrypt the artifact locally. "+
			"Defaults to ~/.ssh/id_ed25519 if present and --ssh-public-key points to the matching .pub.")
	f.BoolVar(&encOpts.generateKey, "generate-key", false,
		"Generate a fresh age X25519 keypair for this run. Writes the identity to "+
			"./migration-identity.age and the recipient to ./migration-recipient.txt. "+
			"Use --generate-key instead of --ssh-public-key when you do not have an existing key. "+
			"Auto-enabled when --encrypt is in effect and no key is supplied.")

	// ── Storage flags ─────────────────────────────────────────────────────────
	f.StringVar(&encOpts.storage, "storage", "artifact",
		`Where to store the (optionally encrypted) bundle after extraction.
artifact (default) — store as a CircleCI job artifact.
s3                 — upload to S3 via the aws CLI (requires AWS creds in job).
both               — store in both artifact and S3.`)
	f.StringVar(&encOpts.s3Bucket, "s3-bucket", "",
		"S3 bucket name for --storage s3|both (required when --storage s3 or both)")
	f.StringVar(&encOpts.s3Prefix, "s3-prefix", "",
		"S3 key prefix for --storage s3|both (optional; e.g. 'migration/')")

	// ── SSH-key extraction flag ───────────────────────────────────────────────
	f.BoolVar(&sshKeysCapture, "ssh-keys", true,
		"Extract additional SSH private keys for projects that have cataloged SSH keys in the manifest. "+
			"Runs a separate in-pipeline job using add_ssh_keys with the explicit cataloged fingerprints — "+
			"the checkout/deploy key is never materialised. "+
			"Use --no-ssh-keys to skip SSH key extraction (e.g. when running env-var capture only).")

	return cmd
}

// resolveEncryptOpts resolves the encryption recipient string from flags:
//   - --generate-key:      generate a fresh age X25519 keypair, write files.
//   - --ssh-public-key:    read the public key from the file.
//
// On success encOpts.recipientStr and encOpts.identityFile are populated.
// SECURITY: private key material is written to file only; never logged.
func resolveEncryptOpts(cmd *cobra.Command, encOpts *captureEncryptOpts) error {
	if encOpts.generateKey && encOpts.sshPublicKey != "" {
		return errors.New("--generate-key and --ssh-public-key are mutually exclusive")
	}
	if !encOpts.generateKey && encOpts.sshPublicKey == "" {
		return errors.New("--encrypt requires --ssh-public-key <path> or --generate-key")
	}

	if encOpts.generateKey {
		// Generate a fresh age X25519 keypair.
		_, idStr, recipStr, err := bundlepkg.GenerateX25519Keypair()
		if err != nil {
			return fmt.Errorf("generating keypair: %w", err)
		}
		idFile := "migration-identity.age"
		recipFile := "migration-recipient.txt"
		// SECURITY: write identity (private key) to 0600 file; do not log.
		if err := writeSecretFile(idFile, idStr+"\n"); err != nil {
			return fmt.Errorf("writing identity file: %w", err)
		}
		if err := writeFile(recipFile, recipStr+"\n"); err != nil {
			return fmt.Errorf("writing recipient file: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(),
			"Generated age X25519 keypair:\n"+
				"  Identity (private key): %s  ← keep secret, needed for decrypt\n"+
				"  Recipient (public key): %s  ← safe to share\n",
			idFile, recipFile)
		encOpts.recipientStr = recipStr
		encOpts.identityFile = idFile
		return nil
	}

	// Read from --ssh-public-key file.
	_, parseErr := bundlepkg.ParseRecipientFile(encOpts.sshPublicKey)
	if parseErr != nil {
		return fmt.Errorf("parsing --ssh-public-key %s: %w", encOpts.sshPublicKey, parseErr)
	}
	// Read raw key string for embedding in config.
	data, err := readFirstKeyLine(encOpts.sshPublicKey)
	if err != nil {
		return fmt.Errorf("reading --ssh-public-key %s: %w", encOpts.sshPublicKey, err)
	}
	encOpts.recipientStr = data

	// Determine identity file for local decryption.
	if encOpts.sshPrivateKey != "" {
		encOpts.identityFile = encOpts.sshPrivateKey
	} else {
		// Default to ~/.ssh/id_ed25519 if it exists.
		home, herr := os.UserHomeDir()
		if herr == nil {
			candidate := home + "/.ssh/id_ed25519"
			if _, serr := os.Stat(candidate); serr == nil {
				encOpts.identityFile = candidate
				clog.Infof("capture: using default SSH identity %s for local decryption", candidate)
			}
		}
	}
	return nil
}

// validateStorageFlags returns an error if the storage-related flags are
// inconsistent (e.g. s3 storage requested but no bucket given).
func validateStorageFlags(encOpts *captureEncryptOpts) error {
	switch encOpts.storage {
	case "artifact", "":
		return nil
	case "s3", "both":
		if encOpts.s3Bucket == "" {
			return fmt.Errorf("--storage %s requires --s3-bucket", encOpts.storage)
		}
		return nil
	default:
		return fmt.Errorf("--storage must be one of: artifact, s3, both (got %q)", encOpts.storage)
	}
}

// writeSecretFile writes data to path with 0600 permissions (private key).
func writeSecretFile(path, data string) error {
	return os.WriteFile(path, []byte(data), 0o600)
}

// writeFile writes data to path with 0644 permissions (public key / recipient).
func writeFile(path, data string) error {
	// #nosec G306 -- recipient file is a PUBLIC age key; world-readable (0644) is intentional
	return os.WriteFile(path, []byte(data), 0o644)
}

// readFirstKeyLine returns the first non-empty, non-comment line from path —
// suitable for extracting the key string from a .pub or age recipients file.
func readFirstKeyLine(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return line, nil
	}
	return "", fmt.Errorf("no key found in %s", path)
}

// selectProjects returns the manifest projects matching slugs.
// When slugs is non-empty, exactly those projects are returned (explicit mode).
// When slugs is empty, only projects that have at least one env var are returned
// (safe default — avoids running pipelines for projects that have nothing to
// capture and prevents accidental full-org sweeps when --project is omitted).
func selectProjects(m *manifest.Manifest, slugs []string) []manifest.Project {
	if len(slugs) == 0 {
		// Default: projects with values only.
		var out []manifest.Project
		for _, p := range m.Projects {
			if len(p.EnvVars) > 0 {
				out = append(out, p)
			}
		}
		return out
	}
	want := make(map[string]bool, len(slugs))
	for _, s := range slugs {
		want[s] = true
	}
	var out []manifest.Project
	for _, p := range m.Projects {
		if want[p.Slug] {
			out = append(out, p)
		}
	}
	return out
}

// findProjectBySlug returns a pointer to the first manifest project whose
// Slug matches slug, or nil if not found.
func findProjectBySlug(m *manifest.Manifest, slug string) *manifest.Project {
	for i := range m.Projects {
		if m.Projects[i].Slug == slug {
			return &m.Projects[i]
		}
	}
	return nil
}

// restrictionDecider is a function that is called when a context with real
// restrictions is encountered.  It should return (true, nil) to remove the
// restrictions temporarily, (false, nil) to skip the context, or (false, err)
// to abort.  A nil decider is treated as "skip" (backward-compatible).
type restrictionDecider func(ctxName string, realRestrictions int) (removeAndRestore bool, err error)

// captureProject handles the full capture flow for a single manifest project.
// It restores the api-trigger-with-config flag even if capture fails.
//
// When projectVarsOnly is true the context loop is skipped entirely — only the
// project's own env vars are captured.  This is the correct mode for the
// per-project env-var loop: context extraction is handled ONCE under the host
// project, never repeated for every project in the manifest.
//
// When projectVarsOnly is false the function works as before: contexts matching
// selectedCtxNames are attached to the pipeline run and their values captured.
//
// restrictDecider is an optional callback that is invoked when a context with
// real restrictions is encountered.  In interactive mode the caller provides a
// prompter-backed decider; in non-interactive mode nil falls back to the
// skipRestricted / removeRestrictions flag logic.
//
// captureSSHKeys controls whether SSH private keys are extracted for this
// project (requires the project to have cataloged SSHKeys in the manifest).
func captureProject(
	ctx context.Context,
	cmd *cobra.Command,
	client captureClient,
	m *manifest.Manifest,
	bundle *manifest.SecretBundle,
	p *manifest.Project,
	selectedCtxNames map[string]bool,
	branch, output string,
	enableTrigger, skipRestricted, removeRestrictions bool,
	pollTimeout time.Duration,
	encOpts captureEncryptOpts,
	restrictDecider restrictionDecider,
	projectVarsOnly bool, // Fix 1: when true, skip context loop entirely
	captureSSHKeys bool, // when true, also extract SSH private keys for this project
) error {
	stderr := cmd.ErrOrStderr()
	stdout := cmd.OutOrStdout()

	// ── 1. Ensure api-trigger-with-config ────────────────────────────────────
	flags, err := client.GetV11ProjectFeatureFlags(p.Slug)
	if err != nil {
		return fmt.Errorf("read feature flags for %s: %w", p.Slug, err)
	}

	wasEnabled := flags[apiTriggerKey]

	if !wasEnabled {
		if !enableTrigger {
			return fmt.Errorf(
				"project %s has api-trigger-with-config disabled; "+
					"set --enable-trigger to enable it automatically "+
					"(both the org-level allow_api_trigger_with_config AND the project-level "+
					"api-trigger-with-config flags must be on for unversioned-config pipelines)",
				p.Slug,
			)
		}
		fmt.Fprintf(stderr, "Enabling api-trigger-with-config for %s…\n", p.Slug)
		if err := client.SetV11ProjectFeatureFlags(p.Slug, map[string]bool{"api-trigger-with-config": true}); err != nil {
			return fmt.Errorf("enable api-trigger-with-config for %s: %w", p.Slug, err)
		}
	}

	// Defer restoration so it runs even on error.
	defer func() {
		if !wasEnabled && enableTrigger {
			fmt.Fprintf(stderr, "Restoring api-trigger-with-config=false for %s…\n", p.Slug)
			if restoreErr := client.SetV11ProjectFeatureFlags(p.Slug, map[string]bool{"api-trigger-with-config": false}); restoreErr != nil {
				fmt.Fprintf(stderr, "WARNING: failed to restore api-trigger-with-config for %s: %v\n", p.Slug, restoreErr)
			}
		}
	}()

	// ── 2. Resolve pipeline definition ID ────────────────────────────────────
	// Bug 4: pre-filter unbuildable projects before triggering a doomed pipeline.
	// Always call the API to get the live definition ID (the manifest struct does
	// not store the definition UUID).  If the API returns no definitions, skip
	// the project with a clear message rather than letting TriggerPipelineRun
	// fail with a cryptic error ("has no pipeline definitions", "github repository
	// not found", "Failed to fetch Branch").
	proj, err := client.GetProject(p.Slug)
	if err != nil {
		return fmt.Errorf("get project %s: %w", p.Slug, err)
	}

	defs, err := client.ListPipelineDefinitions(proj.ID)
	if err != nil {
		return fmt.Errorf("list pipeline definitions for %s: %w", p.Slug, err)
	}
	if len(defs) == 0 {
		displayName := p.Name
		if displayName == "" {
			displayName = p.Slug
		}
		fmt.Fprintf(stderr,
			"SKIP project %s (%s): no pipeline definitions found — "+
				"is the repo connected to a GitHub App? Skipping to avoid a doomed trigger.\n",
			displayName, p.Slug)
		return fmt.Errorf("%w: project %s has no pipeline definitions", errSkipProject, p.Slug)
	}
	defID := defs[0].ID

	// ── 3. Build var name list and context list ───────────────────────────────
	// Project env var names — captured ONLY in project-vars mode. In context
	// mode (projectVarsOnly=false) this project is just the host for context
	// extraction, so we must NOT also dump the host project's own secret values.
	var allVarNames []string
	if projectVarsOnly {
		for _, ev := range p.EnvVars {
			allVarNames = append(allVarNames, ev.Name)
		}
	}

	// Contexts attached to this project (inferred by matching the manifest
	// contexts against the project — the manifest doesn't record explicit
	// project↔context links so we use the selectedCtxNames filter or all).
	var ctxNamesForRun []string

	// Fix 1: when projectVarsOnly=true, skip the context loop entirely.
	// Context extraction happens ONCE under the host project (see RunE).
	// This prevents every per-project pipeline run from re-attaching and
	// re-dumping every context's secrets.
	if !projectVarsOnly {
		for i := range m.Contexts {
			mc := &m.Contexts[i]

			// Apply context filter.  selectedCtxNames is always non-empty here
			// because the caller (RunE) populates it from the manifest for the
			// default-with-values case before reaching the host-project call.
			if !selectedCtxNames[mc.Name] {
				continue
			}

			// Warn about and optionally skip genuinely restricted contexts.
			// The default "All members" group restriction (type==group, value==orgID)
			// is not a real restriction — every App-org context has it automatically.
			real := realRestrictions(mc.Restrictions, m.Source.Org.ID)
			if len(real) > 0 {
				// Bug 5: when a restrictionDecider is provided (interactive mode),
				// ask the user what to do instead of silently skipping.
				if restrictDecider != nil {
					doRemove, decideErr := restrictDecider(mc.Name, len(real))
					if decideErr != nil {
						return decideErr
					}
					if doRemove {
						restore, prepErr := prepareRestrictionRemoval(cmd, client, mc, m.Source.Org.ID)
						if prepErr != nil {
							return prepErr
						}
						defer restore()
					} else {
						fmt.Fprintf(stderr, "Skipping restricted context %q (user chose not to remove restrictions).\n", mc.Name)
						continue
					}
				} else if removeRestrictions {
					// Temporarily remove real restrictions so the extraction run can access
					// the context, then restore from the manifest (source of truth).
					// The restore func is deferred so it runs even on error or panic.
					// The default "All members" group restriction is never touched.
					restore, prepErr := prepareRestrictionRemoval(cmd, client, mc, m.Source.Org.ID)
					if prepErr != nil {
						return prepErr
					}
					defer restore()
				} else {
					fmt.Fprintf(stderr,
						"WARNING: context %q has restrictions (%d). The extraction job may not "+
							"have access to it. Auto-toggling restrictions is not supported; handle "+
							"manually if needed.\n",
						mc.Name, len(real),
					)
					if skipRestricted {
						fmt.Fprintf(stderr, "Skipping restricted context %q (--skip-restricted-contexts=true).\n", mc.Name)
						continue
					}
				}
			}

			ctxNamesForRun = append(ctxNamesForRun, mc.Name)
			for _, ev := range mc.EnvVars {
				allVarNames = append(allVarNames, ev.Name)
			}
		}
	} // end if !projectVarsOnly

	// De-duplicate var names (a context var may shadow a project var with the
	// same name).
	allVarNames = dedupe(allVarNames)

	// ── 4. Run env-var capture (only if there are names to capture) ───────────
	// When there are no env-var names (e.g. a project that has only additional
	// SSH keys and no context/project variables), skip the env-var extraction
	// pipeline entirely: there is nothing to extract, and running it would both
	// waste a pipeline and previously failed on an empty dump — which would also
	// block the SSH-key extraction below. We still fall through to SSH capture.
	capturedVarCount := 0
	if len(allVarNames) > 0 {
		fmt.Fprintf(stdout, "Capturing %d variable(s) for project %s (contexts: %v)…\n",
			len(allVarNames), p.Slug, ctxNamesForRun)

		opts := extract.Options{
			DefinitionID:     defID,
			Branch:           branch,
			PollTimeout:      pollTimeout,
			EncryptRecipient: encOpts.recipientStr,
			Storage:          extract.StorageMode(encOpts.storage),
			S3Bucket:         encOpts.s3Bucket,
			S3Prefix:         encOpts.s3Prefix,
		}

		// SECURITY: encOpts.identityFile is a private key path — do not log.
		values, err := extract.CaptureWithDecrypt(ctx, client, p.Slug, allVarNames, ctxNamesForRun, opts, encOpts.identityFile)
		if err != nil {
			return fmt.Errorf("capture for %s: %w", p.Slug, err)
		}
		capturedVarCount = len(values)

		// ── 5. Store in bundle ────────────────────────────────────────────────
		// Project vars — only in project-vars mode (in context mode this project
		// is just the host and its own vars were not requested above).
		if projectVarsOnly {
			for _, ev := range p.EnvVars {
				if v, ok := values[ev.Name]; ok {
					bundle.SetProjectSecret(p.Slug, ev.Name, v)
				}
			}
		}
		// Context vars.
		for i := range m.Contexts {
			mc := &m.Contexts[i]
			// Only store contexts we actually attached.
			included := false
			for _, n := range ctxNamesForRun {
				if n == mc.Name {
					included = true
					break
				}
			}
			if !included {
				continue
			}
			for _, ev := range mc.EnvVars {
				if v, ok := values[ev.Name]; ok {
					bundle.SetContextSecret(mc.Name, ev.Name, v)
				}
			}
		}
	} else if projectVarsOnly && captureSSHKeys && len(p.SSHKeys) > 0 {
		fmt.Fprintf(stdout, "No env-var values to capture for project %s; proceeding to SSH-key extraction.\n", p.Slug)
	} else {
		fmt.Fprintf(stdout, "No env-var values to capture for project %s; skipping env-var extraction.\n", p.Slug)
	}

	// ── 6. SSH private-key capture (optional) ────────────────────────────────
	// Run a separate in-pipeline job that materialises additional SSH keys via
	// add_ssh_keys (with explicit cataloged fingerprints) and reads the private
	// key files. Only called when the project has cataloged SSH keys AND the
	// caller requested SSH-key extraction.
	if captureSSHKeys && projectVarsOnly && len(p.SSHKeys) > 0 {
		sshInputs := make([]extract.SSHKeyInput, len(p.SSHKeys))
		for i, k := range p.SSHKeys {
			sshInputs[i] = extract.SSHKeyInput{
				Fingerprint: k.Fingerprint,
				Hostname:    k.Hostname,
			}
		}

		fmt.Fprintf(stdout, "Capturing %d SSH key(s) for project %s…\n", len(sshInputs), p.Slug)

		sshOpts := extract.Options{
			DefinitionID:     defID,
			Branch:           branch,
			PollTimeout:      pollTimeout,
			EncryptRecipient: encOpts.recipientStr,
		}

		// SECURITY: encOpts.identityFile is a private key path — do not log.
		captured, sshErr := extract.CaptureSSHKeys(ctx, client, p.Slug, sshInputs, sshOpts, encOpts.identityFile)
		if sshErr != nil {
			// Non-fatal: warn and continue rather than failing the whole capture.
			fmt.Fprintf(stderr, "WARNING: SSH key capture for %s failed: %v\n", p.Slug, sshErr)
		} else {
			for _, k := range captured {
				bundle.AddSSHKey(p.Slug, k)
			}
			fmt.Fprintf(stdout, "Captured %d SSH key(s) for %s\n", len(captured), p.Slug)
		}
	}

	// Write incrementally so a mid-loop failure still saves what was captured.
	bundle.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	bundle.ToolVersion = version.UserAgent()
	if err := bundle.Save(output); err != nil {
		return fmt.Errorf("saving bundle after project %s: %w", p.Slug, err)
	}

	fmt.Fprintf(stdout, "Captured %d variable(s) for %s\n", capturedVarCount, p.Slug)
	return nil
}

// dedupe returns a copy of s with duplicate entries removed (first occurrence
// wins), preserving order.
func dedupe(s []string) []string {
	seen := make(map[string]bool, len(s))
	out := make([]string, 0, len(s))
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
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

// combinedCaptureClient wires a project client (flagReaderWriter,
// pipelineDefLister, projectGetter, extract.Deps) together with a separate
// context client (contextRestrictionManager) into the single captureClient
// interface that captureProject expects.
type combinedCaptureClient struct {
	flagReaderWriter
	pipelineDefLister
	projectGetter
	contextRestrictionManager
	extract.Deps
}

// prepareRestrictionRemoval fetches live restriction IDs for mc, deletes only
// the project and expression restrictions (skipping ALL group restrictions),
// and returns a restore function that re-creates the same project/expression
// restrictions from the manifest.
//
// Org-type restriction matrix (CircleCI API v2):
//
//	restriction_type | GitHub OAuth ("gh/…") | Standalone ("circleci/…") | Bitbucket
//	-----------------+----------------------+---------------------------+----------
//	project          | supported            | supported                 | supported
//	expression       | supported            | supported                 | supported
//	group            | supported            | NOT SUPPORTED             | NOT SUPPORTED
//
// Group restrictions are managed by CircleCI / VCS and are tied to VCS team
// IDs that are org-specific.  Attempting to create a group restriction on a
// non-OAuth org fails with "This is only supported for OAuth orgs."  To avoid
// permanently breaking context access on any org type, this function NEVER
// removes or recreates group restrictions — not even on GitHub OAuth orgs.
// The default "All members" group (type=="group", value==orgID) must especially
// never be deleted.  Any non-default group restrictions in the manifest are
// surfaced to the operator as a manual follow-up notice.
//
// The caller MUST immediately defer the returned restore function.  It runs
// even if extraction later fails or panics.
//
// If any DELETE fails, the error is returned and no restore func is registered
// (nothing was removed yet, nothing needs restoring).
// If a RESTORE fails, a prominent WARNING is printed naming exactly which
// restriction (context, type, value) must be manually re-added.
func prepareRestrictionRemoval(cmd *cobra.Command, client contextRestrictionManager, mc *manifest.Context, orgID string) (restoreFn func(), err error) {
	stderr := cmd.ErrOrStderr()

	// Fetch live restrictions to get their IDs for deletion.
	live, listErr := client.ListRestrictions(mc.SourceID)
	if listErr != nil {
		return func() {}, fmt.Errorf("listing live restrictions for context %q: %w", mc.Name, listErr)
	}

	// Filter live restrictions: only touch project and expression types.
	// ALL group restrictions (including the default "All members" group) are
	// left completely untouched.  Group restrictions are org-type-specific:
	// they can only be created via the API on GitHub OAuth orgs, not on
	// standalone or Bitbucket orgs.  We never delete a group restriction
	// because we might not be able to recreate it.
	var liveToDelete []apicontext.Restriction
	for _, lr := range live {
		if lr.Type == "group" {
			// Group restriction — managed by CircleCI/VCS; never modified by capture.
			fmt.Fprintf(stderr,
				"NOTICE: group restriction on context %q (value=%q) is managed by CircleCI/VCS and is not modified.\n",
				mc.Name, lr.Value,
			)
			continue
		}
		liveToDelete = append(liveToDelete, lr)
	}

	// The restore set comes from the manifest's recorded restrictions, filtered
	// to only project and expression types.  Group restrictions are never
	// re-created — they must be managed manually on the destination.
	var restoreFrom []manifest.Restriction
	for _, r := range mc.Restrictions {
		if isGroupRestriction(r) {
			// Skip group restrictions: not removable/recreatable via API on all org types.
			continue
		}
		if isDefaultAllMembersGroup(r, orgID) {
			// Belt-and-suspenders: skip the All-members default explicitly.
			continue
		}
		restoreFrom = append(restoreFrom, r)
	}

	fmt.Fprintf(stderr,
		"NOTICE: temporarily removing %d project/expression restriction(s) from context %q for extraction.\n",
		len(liveToDelete), mc.Name,
	)
	for _, lr := range liveToDelete {
		if delErr := client.DeleteRestriction(mc.SourceID, lr.ID); delErr != nil {
			return func() {}, fmt.Errorf("deleting restriction %q from context %q: %w", lr.ID, mc.Name, delErr)
		}
	}

	// Build the restore closure.  Re-creates only project/expression restrictions
	// from the manifest.  Group restrictions are never touched.
	restore := func() {
		fmt.Fprintf(stderr,
			"NOTICE: restoring %d project/expression restriction(s) on context %q.\n",
			len(restoreFrom), mc.Name,
		)
		for _, r := range restoreFrom {
			if createErr := client.CreateRestriction(mc.SourceID, r.Type, r.Value); createErr != nil {
				fmt.Fprintf(stderr,
					"WARNING: failed to restore restriction on context %q "+
						"(type=%q value=%q): %v — you must re-add this restriction manually.\n",
					mc.Name, r.Type, r.Value, createErr,
				)
			}
		}
	}
	return restore, nil
}

// maybeEnableOrgTriggerFlag reads the org-level allow_api_trigger_with_config
// flag.  If it is off, it enables it and returns a restore func that must be
// called (typically via defer) to set it back to false.  If it was already on,
// the restore func is a no-op.  On read failure the error is treated as
// best-effort: a WARNING is printed and a no-op restore func is returned so
// the caller can proceed; the per-project flag is the primary gate.
// orgApiTriggerKeyStandalone is the alternate key shape returned by the
// standalone / GitHub-App org settings endpoint.  The trailing "?" is stripped
// before the map lookup (the API sometimes returns keys with a "?" suffix).
const orgApiTriggerKeyStandalone = "allow_api_trigger_with_config_enabled"

// orgTriggerAlreadyEnabled reports whether any of the known key shapes for
// allow_api_trigger_with_config is present and true in the feature-flag map.
// It normalises keys by stripping a trailing "?" (standalone API quirk).
func orgTriggerAlreadyEnabled(flags map[string]bool) bool {
	for k, v := range flags {
		k = strings.TrimSuffix(k, "?")
		if (k == orgApiTriggerKey || k == orgApiTriggerKeyStandalone) && v {
			return true
		}
	}
	return false
}

func maybeEnableOrgTriggerFlag(cmd *cobra.Command, mgr orgFlagManager, vcsType, orgName string) func() {
	stderr := cmd.ErrOrStderr()

	flags, err := mgr.GetFeatureFlags(vcsType, orgName)
	if err != nil {
		fmt.Fprintf(stderr,
			"WARNING: could not read org-level feature flags (%s/%s): %v — proceeding\n",
			vcsType, orgName, err)
		return func() {} // no-op restore
	}

	// Bug 6: tolerate both key shapes (OAuth and standalone) and normalise away
	// the trailing "?" that the standalone endpoint sometimes appends.
	if orgTriggerAlreadyEnabled(flags) {
		clog.Infof("org-level allow_api_trigger_with_config already enabled for %s/%s — skipping enable step", vcsType, orgName)
		return func() {} // already on, nothing to restore
	}

	fmt.Fprintf(stderr, "Enabling org-level allow_api_trigger_with_config for %s/%s…\n", vcsType, orgName)
	if uerr := mgr.UpdateFeatureFlags(vcsType, orgName, map[string]bool{orgApiTriggerKey: true}); uerr != nil {
		fmt.Fprintf(stderr,
			"WARNING: could not enable org-level allow_api_trigger_with_config for %s/%s: %v — proceeding\n",
			vcsType, orgName, uerr)
		return func() {} // failed to enable, nothing to restore
	}

	// Return a restore func that the caller must defer.
	return func() {
		fmt.Fprintf(stderr, "Restoring org-level allow_api_trigger_with_config=false for %s/%s…\n", vcsType, orgName)
		if rerr := mgr.UpdateFeatureFlags(vcsType, orgName, map[string]bool{orgApiTriggerKey: false}); rerr != nil {
			fmt.Fprintf(stderr,
				"WARNING: failed to restore org-level allow_api_trigger_with_config for %s/%s: %v\n",
				vcsType, orgName, rerr)
		}
	}
}

// applyArtifactRetentionControl reads the current org storage-retention controls,
// then sets artifact retention to targetDays (keeping cache/workspace unchanged).
//
// The prior artifact-retention value is logged via clog so the operator knows
// what value to restore if needed. This function deliberately does NOT
// auto-restore: keeping artifact retention low is the safe default when
// secrets may be present in pipeline artifacts (there is no delete-artifact API).
//
// A clear note is printed to stderr with the prior value and restore instructions.
func applyArtifactRetentionControl(cmd *cobra.Command, mgr storageRetentionManager, orgUUID string, targetDays int) {
	stderr := cmd.ErrOrStderr()

	current, err := mgr.GetStorageRetention(orgUUID)
	if err != nil {
		fmt.Fprintf(stderr,
			"WARNING: could not read current artifact-retention for org %s: %v — skipping retention control\n",
			orgUUID, err)
		clog.Warnf("applyArtifactRetentionControl: GetStorageRetention(%s): %v", orgUUID, err)
		return
	}

	priorDays := current.Controls.ArtifactDays
	clog.Infof("artifact-retention safety: current artifact_days=%d, setting to %d for org %s",
		priorDays, targetDays, orgUUID)

	newControls := org.StorageRetentionControls{
		CacheDays:     current.Controls.CacheDays,
		WorkspaceDays: current.Controls.WorkspaceDays,
		ArtifactDays:  targetDays,
	}
	if err := mgr.SetStorageRetention(orgUUID, newControls); err != nil {
		fmt.Fprintf(stderr,
			"WARNING: could not set artifact-retention to %d days for org %s: %v — skipping retention control\n",
			targetDays, orgUUID, err)
		clog.Warnf("applyArtifactRetentionControl: SetStorageRetention(%s): %v", orgUUID, err)
		return
	}

	clog.Infof("artifact-retention set to %d day(s) for org %s (was %d)", targetDays, orgUUID, priorDays)
	fmt.Fprintf(stderr,
		"NOTICE: artifact-retention set to %d day(s) for org %s (was %d day(s)).\n"+
			"  Secrets landing in build artifacts will expire sooner.\n"+
			"  This value is NOT auto-restored. To restore, run:\n"+
			"    POST https://app.circleci.com/private/orgs/%s/storage-retention-controls\n"+
			"    body: {\"retention_days_artifact\":%d,\"retention_days_cache\":%d,\"retention_days_workspace\":%d}\n",
		targetDays, orgUUID, priorDays,
		orgUUID, priorDays, current.Controls.CacheDays, current.Controls.WorkspaceDays,
	)
}
