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
//   - Same-name namespace (DestRunnerNamespace == the source namespace): see
//     syncSameNamespaceRunners — never created while the source org still owns
//     the namespace, and a class that exists but is attributed to another org
//     (class attribution does not follow a namespace transfer) is reported as
//     manual rather than "exists".
//   - Different namespace: after the run a config-rewrite notice lists every
//     `resource_class:` value that must change.
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

	if m.RunnerNamespace != "" && m.RunnerNamespace == opts.DestRunnerNamespace {
		s.syncSameNamespaceRunners(ctx, report, m, opts)
		return report, nil
	}

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

	s.emitRunnerRewriteNotice(report, m, opts.DestRunnerNamespace)
	return report, nil
}

// emitRunnerRewriteNotice lists every source→destination resource-class rename
// when the namespace differs. Config files reference classes by full name, so
// each `resource_class:` value must be rewritten, and agents re-registered
// against the new classes. Informational only: the CLI never edits customer repos.
func (s *Syncer) emitRunnerRewriteNotice(report *Report, m *manifest.Manifest, destNs string) {
	if m.RunnerNamespace == "" || destNs == "" || m.RunnerNamespace == destNs || len(m.RunnerResourceClasses) == 0 {
		return
	}
	var lines []string
	for _, rc := range m.RunnerResourceClasses {
		lines = append(lines, fmt.Sprintf("  %s  →  %s", rc.Name, translateResourceClass(rc.Name, m.RunnerNamespace, destNs)))
	}
	report.add("runner-resource-class", "config-rewrite-notice", "manual", fmt.Sprintf(
		"CONFIG REWRITE REQUIRED: the runner namespace changes (%q → %q), so every `resource_class:` value in .circleci/config.yml "+
			"that references a runner class must be updated, and each launch-agent re-registered with a token for the new class:\n%s",
		m.RunnerNamespace, destNs, strings.Join(lines, "\n")))
	s.logf("")
	s.logf("!! CONFIG REWRITE REQUIRED !!")
	s.logf("Update `resource_class:` values in .circleci/config.yml for each project:")
	for _, l := range lines {
		s.logf("%s", l)
	}
	s.logf("")
}

// syncSameNamespaceRunners handles DestRunnerNamespace == the source namespace.
//
// Class names are unchanged, but a class row stores the org it was created
// under and that attribution does NOT follow a namespace-owner transfer (see
// namespace.go). Decision table:
//
//   - source org still owns the namespace → create nothing; manual action with
//     the exact `change-namespace-owner!` command.
//   - destination owns it (or unknown) → for each class: attributed to the
//     destination org → "exists"; present in the namespace but attributed to
//     another org (the source) → manual; absent → create. A 409 on create is
//     also reported as manual (the class exists under another org).
func (s *Syncer) syncSameNamespaceRunners(ctx context.Context, report *Report, m *manifest.Manifest, opts Options) {
	ns := m.RunnerNamespace
	destOrgID := s.destOrgIDFor(ctx, opts)
	owner := s.detectNamespaceOwner(ctx, ns, m.Source.Org.ID, destOrgID)

	if owner == nsOwnerSource {
		report.add("runner-namespace", ns, "manual", namespaceHandoffDetail("runner", ns, opts)+
			fmt.Sprintf(" After the transfer the %d resource class(es) stay attributed to the source org, so the destination cannot create same-named classes until the source copies are deleted: "+
				"delete each class from the source org (or ask support), re-run `sync`, then issue new agent tokens and re-register the launch-agents.",
				len(m.RunnerResourceClasses)))
		for _, rc := range m.RunnerResourceClasses {
			report.add("runner-resource-class", rc.Name, "skipped",
				fmt.Sprintf("runner resource class %q not created: namespace %q is still owned by the source org", rc.Name, ns))
		}
		return
	}

	if s.Runner == nil && opts.Apply {
		for _, rc := range m.RunnerResourceClasses {
			report.add("runner-resource-class", rc.Name, "manual",
				fmt.Sprintf("runner resource class %q must be created manually (no runner client configured)", rc.Name))
		}
		return
	}

	// Classes attributed to the destination org (nil when it cannot be listed).
	var destAttributed map[string]bool
	if lister, ok := s.Runner.(orgRunnerClassLister); ok && destOrgID != "" {
		if classes, err := lister.ListResourceClassesByOrg(ctx, destOrgID); err != nil {
			clog.Debugf("could not list runner classes for destination org %s: %v", destOrgID, err)
		} else {
			destAttributed = map[string]bool{}
			for _, c := range classes {
				destAttributed[c.ResourceClass] = true
			}
		}
	}

	// Classes present in the namespace regardless of attribution. A failure here
	// when the owner is unknown most likely means the destination does not own
	// the namespace yet.
	inNamespace := map[string]bool{}
	if s.Runner != nil {
		if classes, err := s.Runner.GetResourceClassesByNamespace(ctx, ns); err != nil {
			clog.Debugf("could not list runner classes in namespace %s: %v", ns, err)
			if owner == nsOwnerUnknown && isNamespaceNotOwnedErr(err) {
				report.add("runner-namespace", ns, "manual", fmt.Sprintf(
					"cannot read namespace %q with the destination token (%v) — the namespace is probably still owned by the source org. "+
						"If so, ask CircleCI support to run `%s` (class names, and so config references, stay unchanged), then re-run `sync`.",
					ns, err, handoffCommand(ns, opts)))
				for _, rc := range m.RunnerResourceClasses {
					report.add("runner-resource-class", rc.Name, "skipped",
						fmt.Sprintf("runner resource class %q not created: namespace %q is not accessible to the destination org", rc.Name, ns))
				}
				return
			}
		} else {
			for _, c := range classes {
				inNamespace[c.ResourceClass] = true
			}
		}
	}

	for _, rc := range m.RunnerResourceClasses {
		name := rc.Name
		switch {
		case destAttributed[name]:
			report.add("runner-resource-class", name, "exists",
				fmt.Sprintf("runner resource class %q already exists in the destination org", name))
			continue
		case destAttributed != nil && inNamespace[name]:
			report.add("runner-resource-class", name, "manual", classAttributedElsewhere(name, ns))
			continue
		case destAttributed == nil && inNamespace[name]:
			// Attribution unknown (could not list the destination org's classes):
			// the class is in the namespace, but we cannot prove it is ours.
			report.add("runner-resource-class", name, "exists",
				fmt.Sprintf("runner resource class %q already exists in namespace %q (could not verify it is attributed to the destination org; "+
					"if jobs cannot use it, delete the source copy and re-run sync)", name, ns))
			continue
		}

		if !opts.Apply {
			report.add("runner-resource-class", name, "created",
				fmt.Sprintf("would create runner resource class %q", name))
			continue
		}
		clog.Debugf("CreateResourceClass resource_class=%s", name)
		if _, err := s.Runner.CreateResourceClass(ctx, name, rc.Description); err != nil {
			if isAlreadyExists(err) {
				report.add("runner-resource-class", name, "manual", classAttributedElsewhere(name, ns))
				continue
			}
			report.add("runner-resource-class", name, "error",
				fmt.Sprintf("create runner resource class %q: %v", name, err))
			continue
		}
		report.add("runner-resource-class", name, "created",
			fmt.Sprintf("created runner resource class %q", name))
	}
}

// classAttributedElsewhere is the manual-action text for a same-name class that
// exists but is not attributed to the destination org. runner-admin stores the
// owning org per class at creation time, so it stays with the source org after a
// namespace-owner transfer, and create returns 409 for the duplicate name.
func classAttributedElsewhere(class, ns string) string {
	return fmt.Sprintf(
		"runner resource class %q already exists in namespace %q but is attributed to another org (the source) — class attribution does not follow a namespace transfer. "+
			"Delete it from the source org (it must have no tokens, or force-delete) or ask CircleCI support, then re-run `sync` to recreate it in the destination, "+
			"and issue new agent tokens.",
		class, ns)
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
