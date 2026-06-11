package syncer

import (
	"context"
	"fmt"
	"strings"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/clog"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// SyncRunnerResourceClasses recreates runner resource classes from the manifest
// in the destination namespace specified by opts.DestRunnerNamespace.
//
//   - When DestRunnerNamespace is empty but the manifest has runner classes, each
//     is flagged as "manual" (the syncer never guesses the destination namespace).
//   - When DestRunnerNamespace is set, the class name is translated from
//     "<srcNs>/<name>" → "<destNs>/<name>" before creation.
//   - Idempotent: a class that already exists in the destination namespace is
//     treated as "exists" rather than an error.
//   - Dry-run aware: when opts.Apply is false, planned creations are reported
//     without making any API calls.
func (s *Syncer) SyncRunnerResourceClasses(ctx context.Context, m *manifest.Manifest, opts Options) (*Report, error) {
	report := &Report{Applied: opts.Apply}

	if len(m.RunnerResourceClasses) == 0 {
		clog.Debugf("manifest has no runner resource classes; skipping runner sync")
		return report, nil
	}

	// No destination namespace supplied — flag everything for manual recreation.
	if opts.DestRunnerNamespace == "" {
		s.logf("No --dest-runner-namespace set; runner resource classes require manual recreation")
		for _, rc := range m.RunnerResourceClasses {
			report.add("runner-resource-class", rc.Name, "manual",
				fmt.Sprintf("runner resource class %q must be recreated manually (no --dest-runner-namespace provided)", rc.Name))
		}
		return report, nil
	}

	s.logf("Syncing %d runner resource class(es) → namespace %q%s",
		len(m.RunnerResourceClasses), opts.DestRunnerNamespace, dryRunSuffix(opts.Apply))

	// Build existing-class set in the destination namespace (best-effort: if
	// listing fails we still attempt creation and let the API return 409/conflict).
	existingByName := map[string]bool{}
	if s.Runner != nil && opts.Apply {
		existing, lerr := s.Runner.GetResourceClassesByNamespace(ctx, opts.DestRunnerNamespace)
		if lerr != nil {
			clog.Debugf("could not pre-fetch existing runner classes in %s: %v", opts.DestRunnerNamespace, lerr)
		} else {
			for _, ex := range existing {
				existingByName[ex.ResourceClass] = true
			}
		}
	}

	srcNs := m.RunnerNamespace

	for _, rc := range m.RunnerResourceClasses {
		// Translate "<srcNs>/<name>" → "<destNs>/<name>".
		destName := translateResourceClass(rc.Name, srcNs, opts.DestRunnerNamespace)
		target := destName

		if existingByName[destName] {
			report.add("runner-resource-class", target, "exists",
				fmt.Sprintf("runner resource class %q already exists in destination namespace", destName))
			continue
		}

		if !opts.Apply {
			report.add("runner-resource-class", target, "created",
				fmt.Sprintf("would create runner resource class %q", destName))
			continue
		}

		if s.Runner == nil {
			report.add("runner-resource-class", target, "manual",
				fmt.Sprintf("runner resource class %q must be created manually (no runner client configured)", destName))
			continue
		}

		clog.Debugf("CreateResourceClass resource_class=%s", destName)
		_, err := s.Runner.CreateResourceClass(ctx, destName, rc.Description)
		if err != nil {
			// Treat "already exists" / conflict responses as idempotent success.
			if isAlreadyExists(err) {
				report.add("runner-resource-class", target, "exists",
					fmt.Sprintf("runner resource class %q already exists (conflict on create)", destName))
				continue
			}
			report.add("runner-resource-class", target, "error",
				fmt.Sprintf("create runner resource class %q: %v", destName, err))
			continue
		}
		report.add("runner-resource-class", target, "created",
			fmt.Sprintf("created runner resource class %q", destName))
	}

	return report, nil
}

// translateResourceClass replaces the srcNs portion of a "<ns>/<name>" resource
// class identifier with destNs. When the name does not contain a "/" or the
// source namespace cannot be determined, it falls back to prepending destNs.
func translateResourceClass(name, srcNs, destNs string) string {
	if srcNs != "" && strings.HasPrefix(name, srcNs+"/") {
		return destNs + name[len(srcNs):]
	}
	// Fallback: replace whatever prefix precedes the first "/" with destNs.
	if idx := strings.Index(name, "/"); idx >= 0 {
		return destNs + name[idx:]
	}
	return destNs + "/" + name
}

// isAlreadyExists returns true when err indicates a resource-already-exists
// condition (HTTP 409 Conflict or a message containing "already exists").
func isAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already exists") || strings.Contains(msg, "conflict")
}
