package syncer

import (
	"context"
	"fmt"
	"strings"

	apiOrb "github.com/AwesomeCICD/circleci-org-migration-cli/api/orb"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/clog"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// ─────────────────────────────────────────────────────────────────────────────
// Interfaces
// ─────────────────────────────────────────────────────────────────────────────

// OrbWriter is the subset of the orb client the syncer needs. It mirrors
// the methods on *api/orb.Client so that tests can inject a fake. The method
// signatures use the api/orb types so the production wiring is a direct
// assignment (no adapter needed).
type OrbWriter interface {
	ResolveNamespaceID(ctx context.Context, name string) (string, error)
	CreateOrb(ctx context.Context, shortName, namespaceID string, isPrivate bool) (*apiOrb.OrbPackage, error)
	ResolveVersionRef(ctx context.Context, ref string) (*apiOrb.OrbVersion, error)
	PublishVersion(ctx context.Context, orbID, version, yaml, destRef string) error
}

// OrbFlagManager reads and writes the destination org's orb-publishing feature
// flags (allow_uncertified_public_orbs / allow_private_orbs) via the v1.1
// settings endpoint. It is wired from the cmd layer. The vcsType and orgName
// parameters follow the same convention as OrgSettingsWriter.UpdateFeatureFlags.
type OrbFlagManager interface {
	GetOrbFeatureFlags(ctx context.Context, vcsType, orgName string) (map[string]bool, error)
	UpdateOrbFeatureFlags(ctx context.Context, vcsType, orgName string, flags map[string]bool) error
}

// ─────────────────────────────────────────────────────────────────────────────
// Orb feature-flag key names (v1.1 feature_flags blob)
// ─────────────────────────────────────────────────────────────────────────────

const (
	// flagAllowUncertifiedPublicOrbs enables publishing public (uncertified) orbs.
	flagAllowUncertifiedPublicOrbs = "allow_uncertified_public_orbs"
	// flagAllowPrivateOrbs enables publishing private orbs.
	flagAllowPrivateOrbs = "allow_private_orbs"
)

// ─────────────────────────────────────────────────────────────────────────────
// SyncOrbs
// ─────────────────────────────────────────────────────────────────────────────

// SyncOrbs republishes captured orb versions into the destination namespace.
//
// Behaviour:
//   - When DestOrbNamespace is empty but the manifest has orbs, each is flagged
//     as "manual" with a notice to re-run with --dest-orb-namespace or contact
//     CircleCI support for a namespace transfer.
//   - When DestOrbNamespace is set: resolve dest namespace → for each orb
//     CreateOrb (idempotent) → for each version check existence → publish if absent.
//   - Dry-run aware: when opts.Apply is false, planned actions are reported
//     without making any API calls.
//   - After publishing, a config-reference rewrite notice is printed listing
//     every <src-ns>/<orb> → <dest-ns>/<orb> mapping the operator must update.
//
// The orbFlagManager parameter is optional. When non-nil and opts.Apply is true,
// the syncer reads the current allow_uncertified_public_orbs / allow_private_orbs
// flags before publishing, enables them as needed, and restores the prior values
// after publishing (toggle-and-restore pattern). When nil, the syncer assumes the
// flags are already enabled and proceeds without touching them.
//
// destVCSType and destOrgName are the destination org's VCS type and org name
// (e.g. "github", "acme-new") used by the flag manager's v1.1 API. They are
// ignored when orbFlagManager is nil.
func (s *Syncer) SyncOrbs(ctx context.Context, m *manifest.Manifest, opts Options, orbFlagManager OrbFlagManager, destVCSType, destOrgName string) (*Report, error) {
	report := &Report{Applied: opts.Apply}

	if len(m.Orbs) == 0 {
		clog.Debugf("manifest has no captured orbs; skipping orb sync")
		return report, nil
	}

	// No destination namespace — flag everything as manual.
	if opts.DestOrbNamespace == "" {
		s.logf("No --dest-orb-namespace set; captured orbs require manual action")
		for _, o := range m.Orbs {
			report.add("orb", o.Name, "manual",
				fmt.Sprintf("orb %q must be transferred manually: "+
					"either re-run with --dest-orb-namespace <ns> to republish all versions, "+
					"or submit a support ticket to CircleCI to transfer the namespace %q directly",
					o.Name, m.OrbNamespace))
		}
		return report, nil
	}

	s.logf("Syncing %d orb(s) → namespace %q%s",
		len(m.Orbs), opts.DestOrbNamespace, dryRunSuffix(opts.Apply))

	// Dry run: report planned actions without API calls.
	if !opts.Apply {
		for _, o := range m.Orbs {
			shortName := orbShortName(o.Name)
			destFullName := opts.DestOrbNamespace + "/" + shortName
			for _, ver := range o.Versions {
				report.add("orb-version", destFullName+"@"+ver.Version, "created",
					fmt.Sprintf("would publish %s@%s", destFullName, ver.Version))
			}
		}
		s.emitRewriteNotice(report, m, opts.DestOrbNamespace)
		return report, nil
	}

	// Apply mode: resolve namespace ID first.
	if s.Orb == nil {
		for _, o := range m.Orbs {
			report.add("orb", o.Name, "manual",
				fmt.Sprintf("orb %q must be published manually (no orb client configured)", o.Name))
		}
		return report, nil
	}

	destNsID, err := s.Orb.ResolveNamespaceID(ctx, opts.DestOrbNamespace)
	if err != nil {
		return report, fmt.Errorf("orb sync: resolve destination namespace %q: %w", opts.DestOrbNamespace, err)
	}

	// Toggle-and-restore orb-publishing flags when a manager is provided.
	var (
		hadUncertified bool
		hadPrivate     bool
		needsPrivate   bool
	)
	for _, o := range m.Orbs {
		if o.IsPrivate {
			needsPrivate = true
		}
	}
	if orbFlagManager != nil {
		priorFlags, flagErr := orbFlagManager.GetOrbFeatureFlags(ctx, destVCSType, destOrgName)
		if flagErr != nil {
			// Non-fatal: warn and proceed (flags may already be enabled).
			clog.Debugf("could not read orb feature flags for %s/%s: %v", destVCSType, destOrgName, flagErr)
			s.logf("Warning: could not read destination org orb feature flags: %v", flagErr)
		} else {
			hadUncertified = priorFlags[flagAllowUncertifiedPublicOrbs]
			hadPrivate = priorFlags[flagAllowPrivateOrbs]

			enableFlags := map[string]bool{}
			if !hadUncertified {
				enableFlags[flagAllowUncertifiedPublicOrbs] = true
			}
			if needsPrivate && !hadPrivate {
				enableFlags[flagAllowPrivateOrbs] = true
			}
			if len(enableFlags) > 0 {
				if enErr := orbFlagManager.UpdateOrbFeatureFlags(ctx, destVCSType, destOrgName, enableFlags); enErr != nil {
					clog.Debugf("could not enable orb flags: %v", enErr)
					s.logf("Warning: could not enable orb-publishing flags on destination org: %v", enErr)
				} else {
					// Restore original flag values after publishing.
					defer func() { //nolint:revive
						restore := map[string]bool{}
						if _, wasEnabled := enableFlags[flagAllowUncertifiedPublicOrbs]; wasEnabled {
							restore[flagAllowUncertifiedPublicOrbs] = hadUncertified
						}
						if _, wasEnabled := enableFlags[flagAllowPrivateOrbs]; wasEnabled {
							restore[flagAllowPrivateOrbs] = hadPrivate
						}
						if rErr := orbFlagManager.UpdateOrbFeatureFlags(ctx, destVCSType, destOrgName, restore); rErr != nil {
							clog.Debugf("could not restore orb flags: %v", rErr)
						}
					}()
				}
			}
		}
	}

	// Publish each captured orb.
	for _, o := range m.Orbs {
		shortName := orbShortName(o.Name)
		destFullName := opts.DestOrbNamespace + "/" + shortName

		clog.Debugf("CreateOrb short_name=%s namespace_id=%s private=%v", shortName, destNsID, o.IsPrivate)
		pkg, createErr := s.Orb.CreateOrb(ctx, shortName, destNsID, o.IsPrivate)
		if createErr != nil {
			for _, ver := range o.Versions {
				report.add("orb-version", destFullName+"@"+ver.Version, "error",
					fmt.Sprintf("create orb %q: %v", destFullName, createErr))
			}
			continue
		}

		for _, ver := range o.Versions {
			target := destFullName + "@" + ver.Version
			destRef := destFullName + "@" + ver.Version

			// Idempotency: skip versions already published on the destination.
			existing, checkErr := s.Orb.ResolveVersionRef(ctx, destRef)
			if checkErr != nil {
				report.add("orb-version", target, "error",
					fmt.Sprintf("check existing %s: %v", destRef, checkErr))
				continue
			}
			if existing != nil {
				report.add("orb-version", target, "exists",
					fmt.Sprintf("version %s already published in destination namespace", destRef))
				continue
			}

			clog.Debugf("PublishVersion orb_id=%s version=%s", pkg.ID, ver.Version)
			if pubErr := s.Orb.PublishVersion(ctx, pkg.ID, ver.Version, ver.Source, destRef); pubErr != nil {
				report.add("orb-version", target, "error",
					fmt.Sprintf("publish %s: %v", target, pubErr))
				continue
			}
			report.add("orb-version", target, "created",
				fmt.Sprintf("published %s", target))
		}
	}

	// Emit config-reference rewrite notice for operators.
	s.emitRewriteNotice(report, m, opts.DestOrbNamespace)

	return report, nil
}

// emitRewriteNotice prints a prominent notice listing every source→destination
// orb namespace mapping that operators must update in their .circleci/config.yml
// files. This is always informational — the CLI never rewrites customer repos.
func (s *Syncer) emitRewriteNotice(report *Report, m *manifest.Manifest, destNs string) {
	if m.OrbNamespace == "" || destNs == "" || len(m.Orbs) == 0 {
		return
	}
	if m.OrbNamespace == destNs {
		return
	}

	// Collect unique source→dest orb pairs.
	var lines []string
	for _, o := range m.Orbs {
		shortName := orbShortName(o.Name)
		lines = append(lines, fmt.Sprintf("  %s  →  %s",
			o.Name, destNs+"/"+shortName))
	}

	notice := fmt.Sprintf(
		"CONFIG REWRITE REQUIRED: orb references in .circleci/config.yml must be updated.\n"+
			"The following source→destination orb mappings were published — update every project\n"+
			"config that references these orbs before cutover:\n%s",
		strings.Join(lines, "\n"),
	)
	report.add("orb", "config-rewrite-notice", "manual", notice)
	s.logf("")
	s.logf("!! CONFIG REWRITE REQUIRED !!")
	s.logf("Update orb references in .circleci/config.yml for each project:")
	for _, l := range lines {
		s.logf("%s", l)
	}
	s.logf("")
}

// orbShortName extracts the short (unqualified) orb name from a fully-qualified
// "<namespace>/<name>" string. If there is no slash, the whole string is returned.
func orbShortName(fullName string) string {
	if idx := strings.LastIndex(fullName, "/"); idx >= 0 {
		return fullName[idx+1:]
	}
	return fullName
}
