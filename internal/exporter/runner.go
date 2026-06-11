package exporter

import (
	"context"
	"fmt"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/clog"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// exportRunnerResourceClasses captures self-hosted runner resource classes for
// the namespace named in opts.RunnerNamespace. When the namespace is empty or
// the Runner client is not set, the step is silently skipped. On API error an
// "org"-scoped warning (code "runner_unreadable") is added and the export
// continues — runner classes are never a fatal failure.
func (e *Exporter) exportRunnerResourceClasses(ctx context.Context, m *manifest.Manifest, opts Options) {
	if opts.RunnerNamespace == "" {
		clog.Debugf("runner_namespace not set; skipping runner resource class capture")
		return
	}
	if e.Runner == nil {
		clog.Debugf("Runner client not set; skipping runner resource class capture")
		return
	}

	e.logf("Listing runner resource classes for namespace %q...", opts.RunnerNamespace)
	clog.Debugf("GetResourceClassesByNamespace namespace=%s", opts.RunnerNamespace)

	classes, err := e.Runner.GetResourceClassesByNamespace(ctx, opts.RunnerNamespace)
	if err != nil {
		m.AddWarning("org", "runner_unreadable",
			fmt.Sprintf("could not list runner resource classes for namespace %q: %v", opts.RunnerNamespace, err))
		return
	}

	m.RunnerNamespace = opts.RunnerNamespace
	for _, rc := range classes {
		m.RunnerResourceClasses = append(m.RunnerResourceClasses, manifest.RunnerResourceClass{
			Name:        rc.ResourceClass,
			Description: rc.Description,
		})
		// Runner agent registration tokens are never retrievable via API.
		// Each resource class needs fresh tokens issued on the destination namespace.
		m.AddWarning("runner:"+rc.ResourceClass, "runner_agent_token_excluded",
			fmt.Sprintf("runner resource class %q captured; agent registration tokens are not retrievable via API — issue new tokens on the destination namespace after recreating the class", rc.ResourceClass))
	}

	clog.Debugf("captured %d runner resource class(es) for namespace %s", len(classes), opts.RunnerNamespace)
	e.logf("  → captured %d runner resource class(es)", len(classes))
}
