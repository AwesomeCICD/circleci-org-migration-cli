// Package capture orchestrates CLI-driven, in-pipeline secret extraction across
// a migration manifest. It is the higher-level business logic that sits above
// internal/extract (which runs a single in-pipeline extraction): capture loops
// over manifest projects/contexts, toggles and restores the
// api-trigger-with-config feature flag (project- and org-level), optionally
// removes and restores context restrictions, applies an artifact-retention
// safety control, and stores the captured values into a secret bundle.
//
// The cmd layer constructs CaptureProjectOptions (including io.Writers for
// progress/output) and calls CaptureProject; no cobra types leak into this
// package. No secret values are ever logged.
package capture

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	apicontext "github.com/AwesomeCICD/circleci-org-migration-cli/api/context"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/extract"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/version"
)

// ─────────────────────────────────────────────────────────────────────────────
// Dependency interfaces (injected so tests can use fakes)
// ─────────────────────────────────────────────────────────────────────────────

// FlagReaderWriter is the minimal interface the capture flow needs to read and
// restore the api-trigger-with-config project feature flag. Injected by tests;
// production uses a real *project.Client.
type FlagReaderWriter interface {
	GetV11ProjectFeatureFlags(ctx context.Context, slug string) (map[string]bool, error)
	SetV11ProjectFeatureFlags(ctx context.Context, slug string, flags map[string]bool) error
}

// StorageRetentionManager reads and writes org-level storage-retention controls
// before the extraction pipeline runs. Injected by tests; production uses a
// real *org.Client which satisfies this interface directly.
type StorageRetentionManager interface {
	GetStorageRetention(ctx context.Context, orgUUID string) (*org.StorageRetention, error)
	SetStorageRetention(ctx context.Context, orgUUID string, controls org.StorageRetentionControls) error
}

// OrgFlagManager reads and writes org-level feature flags via the v1.1
// organization settings endpoint. Injected by tests; production uses a real
// *org.Client.
type OrgFlagManager interface {
	GetFeatureFlags(ctx context.Context, vcsType, orgName string) (map[string]bool, error)
	UpdateFeatureFlags(ctx context.Context, vcsType, orgName string, flags map[string]bool) error
}

// PipelineDefLister lists pipeline definitions for a project so the flow can
// resolve the first definition's UUID automatically.
type PipelineDefLister interface {
	ListPipelineDefinitions(ctx context.Context, projectID string) ([]project.PipelineDefinition, error)
}

// ProjectGetter retrieves project metadata (used to get the project UUID).
type ProjectGetter interface {
	GetProject(ctx context.Context, slug string) (*project.Project, error)
}

// ContextRestrictionManager manages context restrictions during capture: it can
// list the live restrictions (to get their IDs for deletion) and create or
// delete individual restrictions. Injected by tests; production uses a real
// *apicontext.Client.
type ContextRestrictionManager interface {
	ListRestrictions(ctx context.Context, contextID string) ([]apicontext.Restriction, error)
	CreateRestriction(ctx context.Context, contextID, restrictionType, restrictionValue string) error
	DeleteRestriction(ctx context.Context, contextID, restrictionID string) error
}

// Client combines all interfaces the capture flow exercises against a single
// project. Production wires a *project.Client (for flags, pipeline defs,
// project metadata and extraction) together with an *apicontext.Client (for
// restrictions) via CombinedClient.
type Client interface {
	FlagReaderWriter
	PipelineDefLister
	ProjectGetter
	ContextRestrictionManager
	extract.Deps
}

// CombinedClient wires a project client (FlagReaderWriter, PipelineDefLister,
// ProjectGetter, extract.Deps) together with a separate context client
// (ContextRestrictionManager) into the single Client interface that
// CaptureProject expects.
type CombinedClient struct {
	FlagReaderWriter
	PipelineDefLister
	ProjectGetter
	ContextRestrictionManager
	extract.Deps
}

// ─────────────────────────────────────────────────────────────────────────────
// Constants
// ─────────────────────────────────────────────────────────────────────────────

const (
	// apiTriggerKey is the project-level feature-flag key for unversioned
	// (inline) config triggers.
	apiTriggerKey = "api-trigger-with-config"

	// OrgAPITriggerKey is the org-level feature-flag key that must also be on
	// for unversioned-config pipeline triggers.
	OrgAPITriggerKey = "allow_api_trigger_with_config"

	// orgAPITriggerKeyStandalone is the alternate key shape returned by the
	// standalone / GitHub-App org settings endpoint. The trailing "?" is
	// stripped before the map lookup (the API sometimes returns keys with a
	// "?" suffix).
	orgAPITriggerKeyStandalone = "allow_api_trigger_with_config_enabled"
)

// ErrSkipProject is a sentinel that CaptureProject returns when a project is
// deliberately skipped (e.g. no pipeline definitions). The outer loop treats
// this as informational rather than a hard error so capture continues for the
// remaining projects.
var ErrSkipProject = errors.New("project skipped")

// ─────────────────────────────────────────────────────────────────────────────
// Encryption / storage options passed in from the cmd layer
// ─────────────────────────────────────────────────────────────────────────────

// EncryptOptions carries the already-resolved encryption and storage settings
// needed to run an in-pipeline extraction. The cmd layer resolves these from
// flags (reading key files, generating keypairs) before calling CaptureProject.
//
// SECURITY: Recipient is a PUBLIC key (safe to embed in the inline config).
// IdentityFile is a path to a PRIVATE key/identity used only for local decrypt;
// it is never logged.
type EncryptOptions struct {
	// Recipient is the resolved age/SSH public key recipient string. Empty
	// means a plaintext artifact.
	Recipient string
	// IdentityFile is the path to the private key/identity used to decrypt the
	// artifact locally. Empty means no local decryption is attempted.
	IdentityFile string
	// Storage is the artifact storage mode ("artifact", "s3" or "both").
	Storage string
	// S3Bucket is the S3 bucket name for s3/both storage modes.
	S3Bucket string
	// S3Prefix is the key prefix within the S3 bucket.
	S3Prefix string
}

// RestrictionDecider is a function that is called when a context with real
// restrictions is encountered. It should return (true, nil) to remove the
// restrictions temporarily, (false, nil) to skip the context, or (false, err)
// to abort. A nil decider is treated as "skip" (backward-compatible).
type RestrictionDecider func(ctxName string, realRestrictions int) (removeAndRestore bool, err error)

// CaptureProjectOptions bundles everything CaptureProject needs to capture a
// single manifest project. It replaces the previous 14-positional-param
// signature. Writers carry progress (Stderr) and result (Stdout) output so the
// cmd layer can inject buffers/cobra streams.
type CaptureProjectOptions struct {
	// Client is the wired API client (project + context clients).
	Client Client

	// Manifest is the full export manifest (provides contexts and org info).
	Manifest *manifest.Manifest
	// Bundle is the secret bundle being populated (saved incrementally).
	Bundle *manifest.SecretBundle
	// Project is the manifest project being captured.
	Project *manifest.Project

	// SelectedCtxNames filters which contexts are attached during a context run.
	// Unused when ProjectVarsOnly is true.
	SelectedCtxNames map[string]bool

	// Branch is the branch to check out for the extraction run.
	Branch string
	// Output is the path of the secret bundle on disk (saved incrementally).
	Output string

	// EnableTrigger enables api-trigger-with-config if not already on, then
	// restores it after capture.
	EnableTrigger bool
	// SkipRestricted skips contexts with real restrictions (warn instead of
	// attempting) when no RestrictDecider and RemoveRestrictions are set.
	SkipRestricted bool
	// RemoveRestrictions temporarily removes real context restrictions before
	// extraction and restores them afterwards.
	RemoveRestrictions bool

	// PollTimeout is the maximum time to wait for each pipeline to complete.
	PollTimeout time.Duration

	// Encrypt holds the resolved encryption/storage settings.
	Encrypt EncryptOptions

	// RestrictDecider is an optional interactive callback for restricted
	// contexts. Nil falls back to the SkipRestricted / RemoveRestrictions logic.
	RestrictDecider RestrictionDecider

	// ProjectVarsOnly skips the context loop entirely — only the project's own
	// env vars are captured. This is the correct mode for the per-project loop:
	// context extraction is handled ONCE under the host project.
	ProjectVarsOnly bool
	// CaptureSSHKeys extracts SSH private keys for this project (requires the
	// project to have cataloged SSHKeys in the manifest).
	CaptureSSHKeys bool

	// Stdout receives result lines ("Captured N variable(s)…").
	Stdout io.Writer
	// Stderr receives progress/warning lines.
	Stderr io.Writer
}

// ─────────────────────────────────────────────────────────────────────────────
// Slug / restriction helpers
// ─────────────────────────────────────────────────────────────────────────────

// ParseOrgSlug converts a manifest org slug into the (vcsType, orgName) pair
// expected by the v1.1 org-settings endpoint.
//
//   - "gh/<org>"        → ("github", "<org>")
//   - "bb/<org>"        → ("bitbucket", "<org>")
//   - "circleci/<uuid>" → ("circleci", "<uuid>")
//
// ok is false when the slug is empty or malformed.
func ParseOrgSlug(slug string) (vcsType, orgName string, ok bool) {
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
// group restriction (type=="group", value==orgID). Every App-org context has
// this restriction automatically; it is NOT a real access restriction.
func isDefaultAllMembersGroup(r manifest.Restriction, orgID string) bool {
	return r.Type == "group" && r.Value == orgID
}

// isGroupRestriction reports whether r is any group restriction
// (type=="group"). Group restrictions are ONLY supported on GitHub OAuth
// ("gh/…") orgs; they cannot be created via the API on standalone
// ("circleci/…") or Bitbucket orgs. The capture flow therefore NEVER removes
// or recreates group restrictions: attempting to do so on a non-OAuth org
// would fail with "This is only supported for OAuth orgs." Only `project` and
// `expression` restrictions are touched during capture.
func isGroupRestriction(r manifest.Restriction) bool {
	return r.Type == "group"
}

// realRestrictions filters out the default "All members" group restriction
// (type=="group" with value==orgID) from the supplied list. Every App-org
// context has this restriction by default; it is NOT a real restriction
// — it simply means "all org members". A context is considered genuinely
// restricted only when at least one non-All-members restriction remains.
//
// NOTE: non-default group restrictions (type=="group", value!=orgID) ARE real
// restrictions and remain in the list so callers can warn about them.
// The remove/restore path in prepareRestrictionRemoval explicitly skips all
// group restrictions (including non-default ones) because they are org-type
// specific: they can only be created on GitHub OAuth orgs, not standalone or
// Bitbucket orgs. Users are directed to re-apply them manually.
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

// SelectProjects returns the manifest projects matching slugs.
// When slugs is non-empty, exactly those projects are returned (explicit mode).
// When slugs is empty, only projects that have at least one env var are returned
// (safe default — avoids running pipelines for projects that have nothing to
// capture and prevents accidental full-org sweeps when --project is omitted).
func SelectProjects(m *manifest.Manifest, slugs []string) []manifest.Project {
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

// FindProjectBySlug returns a pointer to the first manifest project whose Slug
// matches slug, or nil if not found.
func FindProjectBySlug(m *manifest.Manifest, slug string) *manifest.Project {
	for i := range m.Projects {
		if m.Projects[i].Slug == slug {
			return &m.Projects[i]
		}
	}
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

// ─────────────────────────────────────────────────────────────────────────────
// CaptureProject — the per-project orchestration entry point
// ─────────────────────────────────────────────────────────────────────────────

// CaptureProject handles the full capture flow for a single manifest project.
// It restores the api-trigger-with-config flag even if capture fails.
//
// When opts.ProjectVarsOnly is true the context loop is skipped entirely — only
// the project's own env vars are captured. This is the correct mode for the
// per-project env-var loop: context extraction is handled ONCE under the host
// project, never repeated for every project in the manifest.
//
// When opts.ProjectVarsOnly is false the function attaches contexts matching
// opts.SelectedCtxNames to the pipeline run and captures their values.
//
// opts.RestrictDecider is an optional callback invoked when a context with real
// restrictions is encountered. In interactive mode the caller provides a
// prompter-backed decider; in non-interactive mode nil falls back to the
// SkipRestricted / RemoveRestrictions flag logic.
func CaptureProject(ctx context.Context, opts CaptureProjectOptions) error {
	client := opts.Client
	m := opts.Manifest
	bundle := opts.Bundle
	p := opts.Project
	stderr := opts.Stderr
	stdout := opts.Stdout

	// ── 1. Ensure api-trigger-with-config ────────────────────────────────────
	flags, err := client.GetV11ProjectFeatureFlags(ctx, p.Slug)
	if err != nil {
		return fmt.Errorf("read feature flags for %s: %w", p.Slug, err)
	}

	wasEnabled := flags[apiTriggerKey]

	if !wasEnabled {
		if !opts.EnableTrigger {
			return fmt.Errorf(
				"project %s has api-trigger-with-config disabled; "+
					"set --enable-trigger to enable it automatically "+
					"(both the org-level allow_api_trigger_with_config AND the project-level "+
					"api-trigger-with-config flags must be on for unversioned-config pipelines)",
				p.Slug,
			)
		}
		fmt.Fprintf(stderr, "Enabling api-trigger-with-config for %s…\n", p.Slug)
		if err := client.SetV11ProjectFeatureFlags(ctx, p.Slug, map[string]bool{apiTriggerKey: true}); err != nil {
			return fmt.Errorf("enable api-trigger-with-config for %s: %w", p.Slug, err)
		}
	}

	// Defer restoration so it runs even on error.
	defer func() {
		if !wasEnabled && opts.EnableTrigger {
			fmt.Fprintf(stderr, "Restoring api-trigger-with-config=false for %s…\n", p.Slug)
			if restoreErr := client.SetV11ProjectFeatureFlags(ctx, p.Slug, map[string]bool{apiTriggerKey: false}); restoreErr != nil {
				fmt.Fprintf(stderr, "WARNING: failed to restore api-trigger-with-config for %s: %v\n", p.Slug, restoreErr)
			}
		}
	}()

	// ── 2. Resolve pipeline definition ID ────────────────────────────────────
	// Bug 4: pre-filter unbuildable projects before triggering a doomed pipeline.
	// Always call the API to get the live definition ID (the manifest struct does
	// not store the definition UUID). If the API returns no definitions, skip
	// the project with a clear message rather than letting TriggerPipelineRun
	// fail with a cryptic error ("has no pipeline definitions", "github repository
	// not found", "Failed to fetch Branch").
	proj, err := client.GetProject(ctx, p.Slug)
	if err != nil {
		return fmt.Errorf("get project %s: %w", p.Slug, err)
	}

	defs, err := client.ListPipelineDefinitions(ctx, proj.ID)
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
		return fmt.Errorf("%w: project %s has no pipeline definitions", ErrSkipProject, p.Slug)
	}
	defID := defs[0].ID

	// ── 3. Build var name list and context list ───────────────────────────────
	// Project env var names — captured ONLY in project-vars mode. In context
	// mode (ProjectVarsOnly=false) this project is just the host for context
	// extraction, so we must NOT also dump the host project's own secret values.
	var allVarNames []string
	if opts.ProjectVarsOnly {
		for _, ev := range p.EnvVars {
			allVarNames = append(allVarNames, ev.Name)
		}
	}

	// Contexts attached to this project (inferred by matching the manifest
	// contexts against the project — the manifest doesn't record explicit
	// project↔context links so we use the SelectedCtxNames filter or all).
	var ctxNamesForRun []string

	// Fix 1: when ProjectVarsOnly=true, skip the context loop entirely.
	// Context extraction happens ONCE under the host project (see the command).
	// This prevents every per-project pipeline run from re-attaching and
	// re-dumping every context's secrets.
	if !opts.ProjectVarsOnly {
		for i := range m.Contexts {
			mc := &m.Contexts[i]

			// Apply context filter. SelectedCtxNames is always non-empty here
			// because the caller populates it from the manifest for the
			// default-with-values case before reaching the host-project call.
			if !opts.SelectedCtxNames[mc.Name] {
				continue
			}

			// Warn about and optionally skip genuinely restricted contexts.
			// The default "All members" group restriction (type==group, value==orgID)
			// is not a real restriction — every App-org context has it automatically.
			real := realRestrictions(mc.Restrictions, m.Source.Org.ID)
			if len(real) > 0 {
				// Bug 5: when a RestrictDecider is provided (interactive mode),
				// ask the user what to do instead of silently skipping.
				if opts.RestrictDecider != nil {
					doRemove, decideErr := opts.RestrictDecider(mc.Name, len(real))
					if decideErr != nil {
						return decideErr
					}
					if doRemove {
						restore, prepErr := prepareRestrictionRemoval(ctx, stderr, client, mc, m.Source.Org.ID)
						if prepErr != nil {
							return prepErr
						}
						defer restore()
					} else {
						fmt.Fprintf(stderr, "Skipping restricted context %q (user chose not to remove restrictions).\n", mc.Name)
						continue
					}
				} else if opts.RemoveRestrictions {
					// Temporarily remove real restrictions so the extraction run can access
					// the context, then restore from the manifest (source of truth).
					// The restore func is deferred so it runs even on error or panic.
					// The default "All members" group restriction is never touched.
					restore, prepErr := prepareRestrictionRemoval(ctx, stderr, client, mc, m.Source.Org.ID)
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
					if opts.SkipRestricted {
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
	} // end if !ProjectVarsOnly

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

		extractOpts := extract.Options{
			DefinitionID:     defID,
			Branch:           opts.Branch,
			PollTimeout:      opts.PollTimeout,
			EncryptRecipient: opts.Encrypt.Recipient,
			Storage:          extract.StorageMode(opts.Encrypt.Storage),
			S3Bucket:         opts.Encrypt.S3Bucket,
			S3Prefix:         opts.Encrypt.S3Prefix,
		}

		// SECURITY: opts.Encrypt.IdentityFile is a private key path — do not log.
		values, err := extract.CaptureWithDecrypt(ctx, client, p.Slug, allVarNames, ctxNamesForRun, extractOpts, opts.Encrypt.IdentityFile)
		if err != nil {
			return fmt.Errorf("capture for %s: %w", p.Slug, err)
		}
		capturedVarCount = len(values)

		// ── 5. Store in bundle ────────────────────────────────────────────────
		// Project vars — only in project-vars mode (in context mode this project
		// is just the host and its own vars were not requested above).
		if opts.ProjectVarsOnly {
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
	} else if opts.ProjectVarsOnly && opts.CaptureSSHKeys && len(p.SSHKeys) > 0 {
		fmt.Fprintf(stdout, "No env-var values to capture for project %s; proceeding to SSH-key extraction.\n", p.Slug)
	} else {
		fmt.Fprintf(stdout, "No env-var values to capture for project %s; skipping env-var extraction.\n", p.Slug)
	}

	// ── 6. SSH private-key capture (optional) ────────────────────────────────
	// Run a separate in-pipeline job that materialises additional SSH keys via
	// add_ssh_keys (with explicit cataloged fingerprints) and reads the private
	// key files. Only called when the project has cataloged SSH keys AND the
	// caller requested SSH-key extraction.
	if opts.CaptureSSHKeys && opts.ProjectVarsOnly && len(p.SSHKeys) > 0 {
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
			Branch:           opts.Branch,
			PollTimeout:      opts.PollTimeout,
			EncryptRecipient: opts.Encrypt.Recipient,
		}

		// SECURITY: opts.Encrypt.IdentityFile is a private key path — do not log.
		captured, sshErr := extract.CaptureSSHKeys(ctx, client, p.Slug, sshInputs, sshOpts, opts.Encrypt.IdentityFile)
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
	if err := bundle.Save(opts.Output); err != nil {
		return fmt.Errorf("saving bundle after project %s: %w", p.Slug, err)
	}

	fmt.Fprintf(stdout, "Captured %d variable(s) for %s\n", capturedVarCount, p.Slug)
	return nil
}
