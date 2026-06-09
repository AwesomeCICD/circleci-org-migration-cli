// Package syncer writes an exported manifest (plus captured secret values) into
// a destination CircleCI organization. It is idempotent — existing resources are
// reused by name — and defaults to a dry run, recording planned actions in a
// report rather than mutating the org until apply is set.
package syncer

import (
	"fmt"
	"io"

	cctx "github.com/CircleCI-Public/circleci-org-migration-cli/api/context"
	"github.com/CircleCI-Public/circleci-org-migration-cli/internal/manifest"
)

// DefaultPlaceholder is the value used for variables whose real value was not
// captured, when the placeholder policy is selected.
const DefaultPlaceholder = "REPLACE_ME"

// Missing-secret policies.
const (
	MissingSkip        = "skip"
	MissingPlaceholder = "placeholder"
)

// OrgResolver resolves a destination org slug to its UUID.
type OrgResolver interface {
	ResolveOrgID(slug string) (string, error)
}

// ContextWriter is the destination context API the syncer needs.
type ContextWriter interface {
	ListContexts(ownerID, ownerSlug string) ([]cctx.Context, error)
	CreateContext(name, ownerID string) (*cctx.Context, error)
	UpsertEnvVar(contextID, name, value string) error
	ListRestrictions(contextID string) ([]cctx.Restriction, error)
	CreateRestriction(contextID, restrictionType, restrictionValue string) error
}

// Options configures a sync run.
type Options struct {
	// Apply performs writes. When false (the default), the run is a dry run.
	Apply bool
	// MissingSecrets is "skip" (default) or "placeholder".
	MissingSecrets string
	// Placeholder overrides DefaultPlaceholder when the placeholder policy is used.
	Placeholder string
}

func (o Options) placeholder() string {
	if o.Placeholder != "" {
		return o.Placeholder
	}
	return DefaultPlaceholder
}

// Syncer writes into a destination org via the injected clients.
type Syncer struct {
	Org      OrgResolver
	Contexts ContextWriter
	Out      io.Writer
}

func (s *Syncer) logf(format string, args ...any) {
	if s.Out != nil {
		fmt.Fprintf(s.Out, format+"\n", args...)
	}
}

// Action records one planned or performed change.
type Action struct {
	Kind   string // "context" | "context-var" | "restriction"
	Target string // context name (with var/restriction detail in Detail)
	Status string // created | exists | set | skipped | manual | error
	Detail string
}

// Report is the outcome of a sync run.
type Report struct {
	DestOrgSlug string
	DestOrgID   string
	Applied     bool
	Actions     []Action
}

func (r *Report) add(kind, target, status, detail string) {
	r.Actions = append(r.Actions, Action{Kind: kind, Target: target, Status: status, Detail: detail})
}

// Counts returns the number of actions with each status.
func (r *Report) Counts() map[string]int {
	c := map[string]int{}
	for _, a := range r.Actions {
		c[a.Status]++
	}
	return c
}

// SyncContexts recreates the manifest's contexts (and their captured variable
// values and restrictions) in the destination org. The destination org slug is
// mapping.Org.To.
func (s *Syncer) SyncContexts(m *manifest.Manifest, bundle *manifest.SecretBundle, mapping *manifest.Mapping, opts Options) (*Report, error) {
	if mapping == nil {
		mapping = manifest.IdentityMapping(m.Source.Org.Slug)
	}
	destSlug := mapping.Org.To
	if destSlug == "" {
		destSlug = m.Source.Org.Slug
	}
	report := &Report{DestOrgSlug: destSlug, Applied: opts.Apply}

	destOrgID, err := s.Org.ResolveOrgID(destSlug)
	if err != nil {
		return nil, fmt.Errorf("resolving destination org %q: %w", destSlug, err)
	}
	report.DestOrgID = destOrgID
	s.logf("Destination org: %s (id %s)%s", destSlug, destOrgID, dryRunSuffix(opts.Apply))

	existing, err := s.Contexts.ListContexts(destOrgID, "")
	if err != nil {
		return nil, fmt.Errorf("listing destination contexts: %w", err)
	}
	byName := map[string]cctx.Context{}
	for _, c := range existing {
		byName[c.Name] = c
	}

	for _, c := range m.Contexts {
		ctxID, err := s.ensureContext(report, c.Name, destOrgID, byName, opts)
		if err != nil {
			report.add("context", c.Name, "error", err.Error())
			continue
		}
		s.syncContextVars(report, c, bundle, ctxID, opts)
		s.syncContextRestrictions(report, c, ctxID, opts)
	}
	return report, nil
}

// ensureContext returns the destination context ID, creating it if absent.
// In dry-run mode a missing context yields an empty ID (nothing to write into).
func (s *Syncer) ensureContext(report *Report, name, destOrgID string, byName map[string]cctx.Context, opts Options) (string, error) {
	if c, ok := byName[name]; ok {
		report.add("context", name, "exists", "reusing existing context")
		return c.ID, nil
	}
	if !opts.Apply {
		report.add("context", name, "created", "would create context")
		return "", nil
	}
	created, err := s.Contexts.CreateContext(name, destOrgID)
	if err != nil {
		return "", err
	}
	report.add("context", name, "created", "created context")
	byName[name] = *created
	return created.ID, nil
}

func (s *Syncer) syncContextVars(report *Report, c manifest.Context, bundle *manifest.SecretBundle, ctxID string, opts Options) {
	values := map[string]string{}
	if bundle != nil {
		values = bundle.ContextSecrets[c.Name]
	}
	for _, v := range c.EnvVars {
		target := c.Name + "/" + v.Name
		val, ok := values[v.Name]
		if !ok {
			if opts.MissingSecrets == MissingPlaceholder {
				if err := s.writeVar(ctxID, v.Name, opts.placeholder(), opts.Apply); err != nil {
					report.add("context-var", target, "error", err.Error())
					continue
				}
				report.add("context-var", target, "set", "placeholder — value not captured; replace manually")
			} else {
				report.add("context-var", target, "manual", "value not captured; set manually")
			}
			continue
		}
		if err := s.writeVar(ctxID, v.Name, val, opts.Apply); err != nil {
			report.add("context-var", target, "error", err.Error())
			continue
		}
		report.add("context-var", target, "set", "value set from bundle")
	}
}

func (s *Syncer) writeVar(ctxID, name, value string, apply bool) error {
	if !apply || ctxID == "" {
		return nil // dry run, or context that would be created
	}
	return s.Contexts.UpsertEnvVar(ctxID, name, value)
}

func (s *Syncer) syncContextRestrictions(report *Report, c manifest.Context, ctxID string, opts Options) {
	var existing []cctx.Restriction
	if opts.Apply && ctxID != "" {
		if rs, err := s.Contexts.ListRestrictions(ctxID); err == nil {
			existing = rs
		}
	}
	for _, r := range c.Restrictions {
		target := c.Name + " [" + r.Type + "]"
		if r.Type != "expression" {
			// project-type values are source-org UUIDs (need remap); group
			// restriction writes are not GA. Both need manual handling.
			report.add("restriction", target, "manual", fmt.Sprintf("%s restriction %q must be recreated manually", r.Type, restrictionLabel(r)))
			continue
		}
		if hasExpressionRestriction(existing, r.Value) {
			report.add("restriction", target, "exists", "expression restriction already present")
			continue
		}
		if !opts.Apply || ctxID == "" {
			report.add("restriction", target, "set", "would add expression restriction")
			continue
		}
		if err := s.Contexts.CreateRestriction(ctxID, "expression", r.Value); err != nil {
			report.add("restriction", target, "error", err.Error())
			continue
		}
		report.add("restriction", target, "set", "added expression restriction")
	}
}

func hasExpressionRestriction(existing []cctx.Restriction, value string) bool {
	for _, e := range existing {
		if e.Type == "expression" && e.Value == value {
			return true
		}
	}
	return false
}

func restrictionLabel(r manifest.Restriction) string {
	if r.Name != "" {
		return r.Name
	}
	return r.Value
}

func dryRunSuffix(apply bool) string {
	if apply {
		return ""
	}
	return "  [dry run]"
}
