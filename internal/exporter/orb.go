package exporter

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	apiOrb "github.com/AwesomeCICD/circleci-org-migration-cli/api/orb"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/clog"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// OrbAPI is the subset of the orb client the exporter needs.
// When Orb is nil on the Exporter, orb capture is silently skipped.
type OrbAPI interface {
	ResolveNamespaceID(ctx context.Context, name string) (string, error)
	ListOrbs(ctx context.Context, namespaceID string) ([]apiOrb.OrbPackage, error)
	ListStableVersions(ctx context.Context, orbID string) ([]apiOrb.OrbVersion, error)
	GetSource(ctx context.Context, versionID string) (string, error)
}

// exportOrbs captures all published orbs (and their stable versions) for the
// namespace named in opts.OrbNamespace. When the namespace is empty or the
// Orb client is not set, the step is silently skipped. Errors on individual
// orbs or versions are recorded as warnings and do not fail the export —
// orb capture is always best-effort.
func (e *Exporter) exportOrbs(ctx context.Context, m *manifest.Manifest, opts Options) {
	if opts.OrbNamespace == "" {
		clog.Debugf("orb_namespace not set; skipping orb capture")
		return
	}
	if e.Orb == nil {
		clog.Debugf("Orb client not set; skipping orb capture")
		return
	}

	e.logf("Listing orbs for namespace %q...", opts.OrbNamespace)
	clog.Debugf("ResolveNamespaceID namespace=%s", opts.OrbNamespace)

	nsID, err := e.Orb.ResolveNamespaceID(ctx, opts.OrbNamespace)
	if err != nil {
		m.AddWarning("org", "orb_namespace_unresolvable",
			fmt.Sprintf("could not resolve orb namespace %q: %v", opts.OrbNamespace, err))
		return
	}

	orbs, err := e.Orb.ListOrbs(ctx, nsID)
	if err != nil {
		m.AddWarning("org", "orb_list_unreadable",
			fmt.Sprintf("could not list orbs for namespace %q: %v", opts.OrbNamespace, err))
		return
	}

	m.OrbNamespace = opts.OrbNamespace

	totalVersions := 0
	for _, o := range orbs {
		fullName := opts.OrbNamespace + "/" + o.Name
		clog.Debugf("capturing orb %s (private=%v)", fullName, o.IsPrivate)

		versions, verErr := e.Orb.ListStableVersions(ctx, o.ID)
		if verErr != nil {
			m.AddWarning("orb:"+fullName, "orb_versions_unreadable",
				fmt.Sprintf("could not list stable versions for orb %q: %v", fullName, verErr))
			continue
		}

		captured := manifest.CapturedOrb{
			Name:      fullName,
			IsPrivate: o.IsPrivate,
		}

		for _, ver := range versions {
			src, srcErr := e.Orb.GetSource(ctx, ver.ID)
			if srcErr != nil {
				m.AddWarning("orb:"+fullName, "orb_source_unreadable",
					fmt.Sprintf("could not fetch source for %s@%s: %v", fullName, ver.Version, srcErr))
				continue
			}
			captured.Versions = append(captured.Versions, manifest.CapturedOrbVersion{
				Version: ver.Version,
				Source:  src,
			})
		}

		// Sort versions in semver-ascending order so the manifest is stable and
		// the syncer can publish in the correct order.
		sortVersions(captured.Versions)

		if len(captured.Versions) > 0 {
			m.Orbs = append(m.Orbs, captured)
			totalVersions += len(captured.Versions)
		}
	}

	clog.Debugf("captured %d orb(s), %d version(s) for namespace %s", len(m.Orbs), totalVersions, opts.OrbNamespace)
	e.logf("  → captured %d orb(s), %d version(s)", len(m.Orbs), totalVersions)
}

// sortVersions sorts CapturedOrbVersion slices in semver-ascending order using
// a simple major.minor.patch tuple comparison. Pre-release and build-metadata
// suffixes are ignored for ordering purposes. Unparseable versions fall back
// to lexicographic string comparison.
func sortVersions(versions []manifest.CapturedOrbVersion) {
	sort.SliceStable(versions, func(i, j int) bool {
		return semverLess(versions[i].Version, versions[j].Version)
	})
}

// semverLess returns true when a < b in major.minor.patch order.
// It parses only the first three dot-separated numeric components; any
// trailing pre-release label (e.g. "-beta.1") is ignored for ordering.
// Falls back to string comparison if either version is unparseable.
func semverLess(a, b string) bool {
	ta, okA := parseSemverTuple(a)
	tb, okB := parseSemverTuple(b)
	if !okA || !okB {
		return a < b
	}
	for i := 0; i < 3; i++ {
		if ta[i] != tb[i] {
			return ta[i] < tb[i]
		}
	}
	return false // equal
}

// parseSemverTuple extracts [major, minor, patch] from a semver string.
// It strips any leading "v" prefix and any pre-release/build suffix after
// the third component. Returns false when the string is not parseable as
// at least major.minor.patch.
func parseSemverTuple(v string) ([3]int, bool) {
	v = strings.TrimPrefix(v, "v")
	// Strip any pre-release suffix (anything after a '-' following the patch
	// digit) and build metadata ('+').
	if idx := strings.IndexAny(v, "+-"); idx >= 0 {
		v = v[:idx]
	}
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 3 {
		return [3]int{}, false
	}
	var t [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return [3]int{}, false
		}
		t[i] = n
	}
	return t, true
}
