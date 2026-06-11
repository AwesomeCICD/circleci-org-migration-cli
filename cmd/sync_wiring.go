package cmd

import (
	"fmt"
	"io"

	cctx "github.com/AwesomeCICD/circleci-org-migration-cli/api/context"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/runner"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/syncer"
	"github.com/AwesomeCICD/circleci-org-migration-cli/settings"
)

// buildSyncer constructs a *syncer.Syncer wired to the destination org using the
// given token, plus all the cmd-package adapters (org settings, group lister,
// CIAM writer). It is the single source of truth for the ~150 lines of client +
// syncer construction that migrate and sync both need.
//
// out is the writer the syncer streams progress to (cmd.ErrOrStderr() for both
// callers). wireRunner controls whether a runner client is attached: callers pass
// the gating decision (!skipRunner && (destRunnerNamespace != "" || manifest has
// runner classes)) so a runner client is only created when runner sync may run.
//
// The caller builds syncer.Options itself (it is independent of the Syncer
// struct). Behaviour is identical to the prior inline wiring: all four adapters
// are always attached, and the runner client is gated exactly as before. Errors wrap the
// underlying client-construction failure with a stable message; the runner-client
// error keeps the "creating runner client" prefix that callers/tests key on.
func buildSyncer(cfg *settings.Config, token string, out io.Writer, wireRunner bool) (*syncer.Syncer, error) {
	orgClient, err := org.NewClient(cfg, token)
	if err != nil {
		return nil, fmt.Errorf("creating org client: %w", err)
	}
	ctxClient, err := cctx.NewClient(cfg, token)
	if err != nil {
		return nil, fmt.Errorf("creating context client: %w", err)
	}
	projClient, err := project.NewClient(cfg, token)
	if err != nil {
		return nil, fmt.Errorf("creating project client: %w", err)
	}

	sy := &syncer.Syncer{
		Org:         orgClient,
		Contexts:    ctxClient,
		Projects:    projClient,
		OrgSettings: orgSettingsAdapter{orgClient},
		Groups:      orgGroupLister{orgClient},
		CIAM:        ciamWriterAdapter{orgClient},
		Out:         out,
	}

	if wireRunner {
		runnerClient, rerr := runner.NewClient(cfg, token)
		if rerr != nil {
			return nil, fmt.Errorf("creating runner client: %w", rerr)
		}
		sy.Runner = runnerClient
	}

	return sy, nil
}
