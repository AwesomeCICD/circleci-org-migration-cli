// Package transfer orchestrates context env-var transfer via an unversioned
// CircleCI pipeline run in the SOURCE org.
//
// Design (from the CircleCI-Labs/circleci-org-migrator context-secret-transfer
// pattern):
//
//   - A single dynamic/inline pipeline is triggered in the SOURCE org with one
//     job per selected context.  Contexts are org-scoped so one pipeline on any
//     host project can access all of them.
//   - Each job imports the context (CircleCI unmasks the values into the job
//     environment) and PUTs each value straight into the matching context in the
//     DESTINATION org over TLS via the CircleCI API.
//   - NO plaintext ever touches disk or build artifacts — strictly better
//     security than the encrypted-bundle-artifact flow for context vars.
//
// Project env-var design (--include-project-vars):
//
//	Project env vars are STRICTLY project-scoped: they are only injected into a
//	job when the pipeline runs under that exact project.  Therefore, project
//	var transfer uses ONE PIPELINE PER SOURCE PROJECT, each triggered under
//	that project's own slug so CircleCI injects its env vars correctly.  The
//	per-project pipelines are polled concurrently (bounded worker pool).
//
// Trust model:
//
//	The in-pipeline jobs need the DESTINATION org API token so they can PUT
//	values.  The CLI does NOT embed the dest token in plaintext in the generated
//	config YAML.  Instead, the token is expected to be stored in a designated
//	context (or project env var) in the SOURCE org; the inline config references
//	that context by name so CircleCI injects it as an environment variable inside
//	the job.  The CLI emits the context/env-var name into the config, never the
//	token value.
//
//	SECURITY IMPLICATION: Anyone who can administer the SOURCE org (create
//	pipelines, attach contexts, read build logs) has implicit access to anything
//	held in that source context — including the dest token.  This is the same
//	trust level as any other sensitive context in the source org.  Operators
//	should:
//	  1. Use a scoped API token with the minimum permissions needed (write to
//	     destination contexts only).
//	  2. Rotate the dest token after the transfer is complete.
//	  3. Restrict the source context that holds the dest token to the minimal
//	     set of pipelines/projects that need it.
//
// Dry-run / apply gating:
//
//	By default (DryRun: true) Transfer performs NO writes — it logs the plan:
//	which contexts and variables would be transferred, and which source context
//	holds the dest token.  Pass DryRun: false (--apply) to execute the pipeline
//	and perform the actual transfer.
//
// No secret values are ever logged.
package transfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	apicontext "github.com/AwesomeCICD/circleci-org-migration-cli/api/context"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/ui"
)

// ─────────────────────────────────────────────────────────────────────────────
// Dependency interfaces (injected so tests can use fakes)
// ─────────────────────────────────────────────────────────────────────────────

// PipelineRunner triggers an unversioned pipeline run and returns its UUID.
type PipelineRunner interface {
	TriggerPipelineRun(ctx context.Context, slug, definitionID, branch, configYAML string, params map[string]any) (string, error)
}

// WorkflowPoller returns the current workflows for a pipeline.
type WorkflowPoller interface {
	GetPipelineWorkflows(ctx context.Context, pipelineID string) ([]project.Workflow, error)
	GetPipeline(ctx context.Context, pipelineID string) (*project.Pipeline, error)
}

// PipelineDefLister lists pipeline definitions for a project.
type PipelineDefLister interface {
	ListPipelineDefinitions(ctx context.Context, projectID string) ([]project.PipelineDefinition, error)
}

// ProjectGetter retrieves project metadata (used to get the project UUID).
type ProjectGetter interface {
	GetProject(ctx context.Context, slug string) (*project.Project, error)
}

// ContextRestrictionManager manages context restrictions for the transfer flow.
// It can list live restrictions (to get their IDs), and create/delete them.
// Injected by tests; production uses a real *apicontext.Client.
type ContextRestrictionManager interface {
	ListRestrictions(ctx context.Context, contextID string) ([]apicontext.Restriction, error)
	CreateRestriction(ctx context.Context, contextID, restrictionType, restrictionValue string) error
	DeleteRestriction(ctx context.Context, contextID, restrictionID string) error
}

// Deps bundles all source-side API dependencies so callers can pass a single
// concrete *project.Client or a fake in tests.
type Deps interface {
	PipelineRunner
	WorkflowPoller
	PipelineDefLister
	ProjectGetter
}

// ─────────────────────────────────────────────────────────────────────────────
// Options
// ─────────────────────────────────────────────────────────────────────────────

// Options controls the behaviour of Transfer.
type Options struct {
	// HostProjectSlug is the source-org project that the inline pipeline runs
	// under.  Any project works — build history is irrelevant, only the pipeline
	// execution environment matters.
	HostProjectSlug string

	// Branch is checked out for the transfer run (default "main").
	Branch string

	// DestHost is the destination org's CircleCI host URL (default "https://circleci.com").
	// Required for server installations.
	DestHost string

	// DestOrgID is the destination org UUID (required).  Used by the in-pipeline
	// job to resolve dest context IDs by listing all contexts for the dest org.
	DestOrgID string

	// DestTokenContext is the NAME of the source-org context that holds the
	// destination API token.  The token must be stored in that context as the
	// environment variable named by DestTokenEnvVar.
	//
	// SECURITY: the CLI embeds DestTokenContext (the CONTEXT NAME) in the config
	// so that CircleCI attaches the context to the job and injects its variables.
	// The token VALUE never appears in the config — it remains inside CircleCI.
	DestTokenContext string

	// DestTokenEnvVar is the env-var name inside DestTokenContext that holds the
	// destination API token (default "CIRCLECI_DEST_TOKEN").
	DestTokenEnvVar string

	// SelectedContextNames is the set of source context names to transfer.
	// Empty means all contexts with at least one variable.
	SelectedContextNames map[string]bool

	// Mapping is an optional source→dest context/project name mapping.  When a
	// source context name has an entry (key without "/"), the destination context
	// is looked up by the mapped name.  When a source project slug has an entry
	// (key containing "/"), the destination project slug is the value.
	Mapping map[string]string

	// IncludeProjectVars controls whether project env-var values are also
	// transferred.  Default false (context-only).  When true, each source project
	// that has env vars AND can be resolved to a destination project slug is
	// included in the transfer pipeline.  Projects without a resolvable dest slug
	// are skipped with a WARN line in the plan.
	IncludeProjectVars bool

	// IncludeSSHKeys controls whether additional project SSH keys are also
	// transferred via the in-pipeline zero-disk path (add_ssh_keys materializes
	// each key; the script reads + POSTs it to the destination).  Default false.
	// When true, each source project that has SSH keys AND can be resolved to a
	// destination project slug is included in (or added to) the per-project
	// pipeline.  A project with only SSH keys (no env vars, or IncludeProjectVars
	// is off) will still get a per-project pipeline when IncludeSSHKeys is true.
	IncludeSSHKeys bool

	// RemoveRestrictions controls whether project/expression restrictions on
	// source contexts are temporarily removed before the transfer pipeline and
	// restored afterwards.  When false (the default) and blocking restrictions
	// are detected, Transfer fails fast with an actionable error rather than
	// triggering a pipeline that will come back "unauthorized".
	//
	// The default "All members" group restriction is NEVER removed — it is not a
	// real restriction and every App-org context has it automatically.  Non-default
	// group restrictions are also never removed (they are org-type specific and
	// cannot always be recreated via API).
	//
	// Requires ContextClient to be set in Options.
	RemoveRestrictions bool

	// ContextClient is the source-org context API client used to list, delete, and
	// recreate restrictions when RemoveRestrictions is true.  May be nil when
	// RemoveRestrictions is false.
	ContextClient ContextRestrictionManager

	// DryRun controls whether the transfer is actually executed.
	// When true (the default), only a plan is printed and no pipeline is triggered.
	// Pass DryRun: false (--apply) to execute.
	DryRun bool

	// PollInterval is the delay between workflow-status polls (default 10s).
	PollInterval time.Duration

	// PollTimeout is the maximum time to wait for the pipeline to finish.
	// Zero means no timeout (the caller's context deadline applies instead).
	PollTimeout time.Duration

	// Stdout receives result/plan lines.
	Stdout io.Writer

	// Stderr receives progress/warning lines.
	Stderr io.Writer
}

// branchFor resolves the branch to check out for a project's trigger pipeline.
//
// Precedence:
//  1. An explicit Options.Branch (the --branch flag) wins for ALL projects —
//     this preserves the override behaviour for the rare case where the operator
//     wants to force one branch across every project.
//  2. Otherwise the project's own defaultBranch (vcs.default_branch from the
//     manifest) is used, so orgs with mixed default branches (some main, some
//     master) each trigger on the correct branch.
//  3. Otherwise fall back to "main".
func (o *Options) branchFor(defaultBranch string) string {
	if o.Branch != "" {
		return o.Branch
	}
	if defaultBranch != "" {
		return defaultBranch
	}
	return "main"
}

func (o *Options) destHost() string {
	if o.DestHost != "" {
		return o.DestHost
	}
	return "https://circleci.com"
}

func (o *Options) destTokenEnvVar() string {
	if o.DestTokenEnvVar != "" {
		return o.DestTokenEnvVar
	}
	return "CIRCLECI_DEST_TOKEN"
}

func (o *Options) pollInterval() time.Duration {
	if o.PollInterval > 0 {
		return o.PollInterval
	}
	return 10 * time.Second
}

// destContextName returns the name to use in the destination org for a given
// source context name, consulting Mapping if present.
func (o *Options) destContextName(srcName string) string {
	if o.Mapping != nil {
		if dst, ok := o.Mapping[srcName]; ok {
			return dst
		}
	}
	return srcName
}

// destProjectSlug returns the destination project slug for a given source
// project slug, consulting Mapping if present.  ok is false when the slug
// cannot be resolved (caller should skip and flag the project).
func (o *Options) destProjectSlug(srcSlug string) (string, bool) {
	if o.Mapping != nil {
		if dst, ok := o.Mapping[srcSlug]; ok {
			return dst, true
		}
	}
	// No mapping entry — we cannot derive the dest slug automatically.
	return "", false
}

// ─────────────────────────────────────────────────────────────────────────────
// Plan — what would be transferred
// ─────────────────────────────────────────────────────────────────────────────

// ContextPlan describes what would be transferred for one context.
type ContextPlan struct {
	// SourceName is the context name in the source org.
	SourceName string
	// DestName is the context name in the destination org (may differ via Mapping).
	DestName string
	// VarNames are the env-var names that would be transferred.
	VarNames []string
	// WillCreate is true when the destination context is expected to be absent
	// and will be created by the in-pipeline job before setting values.
	// False means the job will attempt to look it up (update path).
	WillCreate bool
	// BlockingRestrictions lists the project/expression restrictions (from the
	// manifest) that would prevent the transfer pipeline from accessing this
	// context.  Empty means the context is freely accessible.
	// The default "All members" group restriction is never included here.
	BlockingRestrictions []manifest.Restriction
}

// SSHKeyPlan describes one additional SSH key that would be transferred for a project.
type SSHKeyPlan struct {
	// Fingerprint is the bare SHA256 fingerprint (no "SHA256:" prefix).
	Fingerprint string
	// Hostname is the target host this key is scoped to (may be empty for global keys).
	Hostname string
}

// ProjectVarPlan describes what would be transferred for one project's env vars
// (and optionally additional SSH keys).
type ProjectVarPlan struct {
	// SourceSlug is the project slug in the source org.
	SourceSlug string
	// DestSlug is the resolved project slug in the destination org.
	// Empty when Skipped is true.
	DestSlug string
	// VarNames are the env-var names that would be transferred.
	VarNames []string
	// SSHKeys are the additional SSH keys to transfer in-pipeline for this project.
	// Only populated when IncludeSSHKeys is set in Options.
	SSHKeys []SSHKeyPlan
	// DefaultBranch is the project's recorded default branch (vcs.default_branch
	// from the manifest). Used to resolve the per-project trigger branch when the
	// user did not pass an explicit --branch. Empty when the manifest had none.
	DefaultBranch string
	// Skipped is true when the destination project cannot be resolved.
	Skipped bool
	// SkipReason is a human-readable explanation of why Skipped is true.
	SkipReason string
}

// Plan describes what Transfer would do (dry-run output).
type Plan struct {
	// Contexts is the ordered list of contexts that would be transferred.
	Contexts []ContextPlan
	// Projects is the ordered list of project env-var plans (populated only
	// when IncludeProjectVars is set).
	Projects []ProjectVarPlan
	// DestTokenContext is the source context holding the dest token.
	DestTokenContext string
	// DestTokenEnvVar is the env-var name within DestTokenContext.
	DestTokenEnvVar string
}

// TotalVars returns the total number of env-var values in the plan (contexts only).
func (p *Plan) TotalVars() int {
	n := 0
	for _, c := range p.Contexts {
		n += len(c.VarNames)
	}
	return n
}

// TotalProjectVars returns the total number of project env-var values in the plan.
func (p *Plan) TotalProjectVars() int {
	n := 0
	for _, pv := range p.Projects {
		if !pv.Skipped {
			n += len(pv.VarNames)
		}
	}
	return n
}

// TotalSSHKeys returns the total number of SSH keys to transfer across all projects.
func (p *Plan) TotalSSHKeys() int {
	n := 0
	for _, pv := range p.Projects {
		if !pv.Skipped {
			n += len(pv.SSHKeys)
		}
	}
	return n
}

// ─────────────────────────────────────────────────────────────────────────────
// Config builder
// ─────────────────────────────────────────────────────────────────────────────

// transferJobName is the base name for per-context transfer jobs.
const transferJobName = "circleci-migrate-transfer"

// buildTransferConfig constructs the inline CircleCI YAML config that:
//   - Has one job per selected context.
//   - Each job attaches the source context (so CircleCI injects its values),
//     AND the dest-token context (so the job has the destination API token).
//   - Each job installs circleci-migrate and runs a shell script that PUTs each
//     env-var value to the matching context in the destination org via the
//     CircleCI API.
//   - Optionally, one job per project (when IncludeProjectVars is set) PUTs
//     project env-var values to the matching project in the destination org.
//
// Design invariants:
//   - The dest token value NEVER appears in the generated YAML — it is
//     referenced only by env-var name (${CIRCLECI_DEST_TOKEN} or the override).
//   - No secret values are written to any file or artifact.
//   - The PUT calls go directly over TLS to the destination API.
func buildTransferConfig(m *manifest.Manifest, ctxPlans []ContextPlan, projPlans []ProjectVarPlan, opts *Options) string {
	return buildTransferConfigWithVersion(m, ctxPlans, projPlans, opts)
}

// buildTransferConfigWithVersion is the testable variant (kept for test backward-compat).
func buildTransferConfigWithVersion(m *manifest.Manifest, ctxPlans []ContextPlan, projPlans []ProjectVarPlan, opts *Options) string {
	destHost := opts.destHost()
	destTokenEnvVar := opts.destTokenEnvVar()
	destOrgID := opts.DestOrgID
	destTokenCtx := opts.DestTokenContext

	var sb strings.Builder

	sb.WriteString("version: 2.1\n")
	sb.WriteString("jobs:\n")

	// ── Context jobs ─────────────────────────────────────────────────────────

	for _, cp := range ctxPlans {
		if len(cp.VarNames) == 0 {
			continue
		}

		jobName := transferJobName + "-" + sanitizeName(cp.SourceName)

		sb.WriteString("  " + jobName + ":\n")
		sb.WriteString("    docker:\n")
		sb.WriteString("      - image: cimg/base:current\n")
		sb.WriteString("    resource_class: small\n")
		sb.WriteString("    steps:\n")

		// Transfer step: for each env-var, resolve the dest context ID (via the
		// dest API using the dest token), then PUT the value.
		// The dest token is available as ${CIRCLECI_DEST_TOKEN} (or custom name)
		// from the dest-token context attached at the workflow level.
		//
		// Security design:
		//   - Values are read from environment (injected by the source context).
		//   - PUT requests go directly to the dest API over TLS.
		//   - `set -euo pipefail` plus `|| true` on the value echo ensures no
		//     value leaks via a partial write or log truncation.
		//   - We never log or echo values; curl is called with -s (silent).
		//   - The resolved context ID from the dest API is not a secret.
		sb.WriteString("      - run:\n")
		sb.WriteString(fmt.Sprintf("          name: Transfer env vars for context %q\n", cp.SourceName))
		sb.WriteString("          command: |\n")
		sb.WriteString("            set -euo pipefail\n")
		sb.WriteString("\n")

		// Resolve destination context ID.
		sb.WriteString(fmt.Sprintf("            DEST_HOST=%q\n", destHost))
		sb.WriteString(fmt.Sprintf("            DEST_ORG_ID=%q\n", destOrgID))
		sb.WriteString(fmt.Sprintf("            DEST_CTX_NAME=%q\n", cp.DestName))
		sb.WriteString(fmt.Sprintf("            DEST_TOKEN_VAR=%q\n", destTokenEnvVar))
		sb.WriteString(fmt.Sprintf("            DEST_TOKEN=${%s:?%q env var is required (should be in the dest-token context)}\n",
			destTokenEnvVar, destTokenEnvVar))
		sb.WriteString("\n")
		sb.WriteString("            # Resolve dest context ID by listing contexts for the dest org.\n")
		sb.WriteString("            # The list endpoint returns contexts paginated; we iterate pages.\n")
		sb.WriteString("            DEST_CTX_ID=''\n")
		sb.WriteString("            page_token=''\n")
		sb.WriteString("            while true; do\n")
		sb.WriteString("              url=\"${DEST_HOST}/api/v2/context?owner-id=${DEST_ORG_ID}\"\n")
		sb.WriteString("              if [ -n \"$page_token\" ]; then\n")
		sb.WriteString("                url=\"${url}&page-token=${page_token}\"\n")
		sb.WriteString("              fi\n")
		sb.WriteString("              resp=$(curl -sf -H \"Circle-Token: ${DEST_TOKEN}\" \"${url}\")\n")
		sb.WriteString("              DEST_CTX_ID=$(printf '%s' \"$resp\" | jq -r --arg name \"$DEST_CTX_NAME\" '.items[] | select(.name==$name) | .id' | head -1)\n")
		sb.WriteString("              if [ -n \"$DEST_CTX_ID\" ]; then break; fi\n")
		sb.WriteString("              next_token=$(printf '%s' \"$resp\" | jq -r '.next_page_token // empty')\n")
		sb.WriteString("              if [ -z \"$next_token\" ]; then break; fi\n")
		sb.WriteString("              page_token=\"$next_token\"\n")
		sb.WriteString("            done\n")
		sb.WriteString("\n")
		// Create-if-missing: when the context is not found, POST to create it.
		sb.WriteString("            if [ -z \"$DEST_CTX_ID\" ]; then\n")
		sb.WriteString(fmt.Sprintf("              echo \"Destination context %q not found — creating it in org ${DEST_ORG_ID}\"\n", cp.DestName))
		sb.WriteString("              create_body=$(jq -n --arg name \"$DEST_CTX_NAME\" --arg oid \"$DEST_ORG_ID\" \\\n")
		sb.WriteString("                '{\"name\": $name, \"owner\": {\"id\": $oid, \"type\": \"organization\"}}')\n")
		sb.WriteString("              create_resp=$(curl -sf \\\n")
		sb.WriteString("                -X POST \\\n")
		sb.WriteString("                -H 'Content-Type: application/json' \\\n")
		sb.WriteString("                -H \"Circle-Token: ${DEST_TOKEN}\" \\\n")
		sb.WriteString("                -d \"$create_body\" \\\n")
		sb.WriteString("                \"${DEST_HOST}/api/v2/context\")\n")
		sb.WriteString("              DEST_CTX_ID=$(printf '%s' \"$create_resp\" | jq -r '.id')\n")
		sb.WriteString("              if [ -z \"$DEST_CTX_ID\" ] || [ \"$DEST_CTX_ID\" = 'null' ]; then\n")
		sb.WriteString(fmt.Sprintf("                echo \"ERROR: failed to create destination context %q\" >&2\n", cp.DestName))
		sb.WriteString("                exit 1\n")
		sb.WriteString("              fi\n")
		sb.WriteString(fmt.Sprintf("              echo \"Created destination context %q → ${DEST_CTX_ID}\"\n", cp.DestName))
		sb.WriteString("            else\n")
		sb.WriteString(fmt.Sprintf("              echo \"Resolved destination context %q → ${DEST_CTX_ID}\"\n", cp.DestName))
		sb.WriteString("            fi\n")
		sb.WriteString("\n")

		// PUT each env var.
		sb.WriteString("            # PUT each env-var value to the destination context.\n")
		sb.WriteString("            # Values are read from the job environment (injected by the source context).\n")
		sb.WriteString("            # curl -s: silent; -o /dev/null: discard response body on success;\n")
		sb.WriteString("            # -w: print HTTP status for error checking.\n")
		sb.WriteString("            transfer_ok=true\n")
		for _, varName := range cp.VarNames {
			// Shell-safe variable name (already validated to be env-var format).
			safeVar := strings.ReplaceAll(varName, "'", "'\\''")
			sb.WriteString(fmt.Sprintf("            # Transfer %s\n", varName))
			sb.WriteString(fmt.Sprintf("            val=${%s:-}\n", safeVar))
			// Build the JSON body using printf + jq so the value is never interpolated
			// directly into a shell string — prevents shell injection via malformed values.
			sb.WriteString("            body=$(jq -n --arg v \"$val\" '{\"value\": $v}')\n")
			sb.WriteString("            http_code=$(curl -s -o /dev/null -w '%{http_code}' \\\n")
			sb.WriteString("              -X PUT \\\n")
			sb.WriteString("              -H 'Content-Type: application/json' \\\n")
			sb.WriteString("              -H \"Circle-Token: ${DEST_TOKEN}\" \\\n")
			sb.WriteString("              -d \"$body\" \\\n")
			sb.WriteString(fmt.Sprintf("              \"${DEST_HOST}/api/v2/context/${DEST_CTX_ID}/environment-variable/%s\")\n", varName))
			sb.WriteString("            if [ \"$http_code\" != '200' ]; then\n")
			sb.WriteString(fmt.Sprintf("              echo \"ERROR: PUT %s HTTP ${http_code}\" >&2\n", varName))
			sb.WriteString("              transfer_ok=false\n")
			sb.WriteString("            else\n")
			sb.WriteString(fmt.Sprintf("              echo \"Transferred: %s\"\n", varName))
			sb.WriteString("            fi\n")
		}
		sb.WriteString("            if [ \"$transfer_ok\" = 'false' ]; then\n")
		sb.WriteString("              echo 'ERROR: one or more env-var PUTs failed (see above).' >&2\n")
		sb.WriteString("              exit 1\n")
		sb.WriteString("            fi\n")
		sb.WriteString(fmt.Sprintf("            echo 'Transfer complete for context %q'\n", cp.SourceName))
		sb.WriteString("\n")
	}

	// ── Project env-var jobs ──────────────────────────────────────────────────

	const projJobName = "circleci-migrate-transfer-project"

	for _, pp := range projPlans {
		if pp.Skipped || len(pp.VarNames) == 0 {
			continue
		}

		jobName := projJobName + "-" + sanitizeName(pp.SourceSlug)

		sb.WriteString("  " + jobName + ":\n")
		sb.WriteString("    docker:\n")
		sb.WriteString("      - image: cimg/base:current\n")
		sb.WriteString("    resource_class: small\n")
		sb.WriteString("    steps:\n")

		// Project env-var transfer step.
		// The source project's env vars are available in the job environment
		// because the job runs under that project (they are injected by CircleCI).
		// We PUT/POST each value to the destination project via the v1.1 API.
		//
		// CircleCI project env-var API:
		//   POST /api/v1.1/project/{slug}/envvar   → 201 (create or update)
		//   PUT  /api/v1.1/project/{slug}/envvar   → not available; POST is idempotent-upsert
		// We use POST (add-or-update) which returns 201 on create and 200 on update.
		sb.WriteString("      - run:\n")
		sb.WriteString(fmt.Sprintf("          name: Transfer project env vars for %q\n", pp.SourceSlug))
		sb.WriteString("          command: |\n")
		sb.WriteString("            set -euo pipefail\n")
		sb.WriteString("\n")
		sb.WriteString(fmt.Sprintf("            DEST_HOST=%q\n", destHost))
		sb.WriteString(fmt.Sprintf("            DEST_PROJECT_SLUG=%q\n", pp.DestSlug))
		sb.WriteString(fmt.Sprintf("            DEST_TOKEN=${%s:?%q env var is required (should be in the dest-token context)}\n",
			destTokenEnvVar, destTokenEnvVar))
		sb.WriteString("\n")
		sb.WriteString("            # POST each project env var to the destination project.\n")
		sb.WriteString("            # Values are available in the job environment from the source project.\n")
		sb.WriteString("            transfer_ok=true\n")
		for _, varName := range pp.VarNames {
			safeVar := strings.ReplaceAll(varName, "'", "'\\''")
			sb.WriteString(fmt.Sprintf("            # Transfer project var %s\n", varName))
			sb.WriteString(fmt.Sprintf("            val=${%s:-}\n", safeVar))
			sb.WriteString("            body=$(jq -n --arg n \"" + varName + "\" --arg v \"$val\" '{\"name\": $n, \"value\": $v}')\n")
			sb.WriteString("            http_code=$(curl -s -o /dev/null -w '%{http_code}' \\\n")
			sb.WriteString("              -X POST \\\n")
			sb.WriteString("              -H 'Content-Type: application/json' \\\n")
			sb.WriteString("              -H \"Circle-Token: ${DEST_TOKEN}\" \\\n")
			sb.WriteString("              -d \"$body\" \\\n")
			sb.WriteString("              \"${DEST_HOST}/api/v1.1/project/${DEST_PROJECT_SLUG}/envvar\")\n")
			sb.WriteString("            if [ \"$http_code\" != '201' ] && [ \"$http_code\" != '200' ]; then\n")
			sb.WriteString(fmt.Sprintf("              echo \"ERROR: POST project var %s HTTP ${http_code}\" >&2\n", varName))
			sb.WriteString("              transfer_ok=false\n")
			sb.WriteString("            else\n")
			sb.WriteString(fmt.Sprintf("              echo \"Transferred project var: %s\"\n", varName))
			sb.WriteString("            fi\n")
		}
		sb.WriteString("            if [ \"$transfer_ok\" = 'false' ]; then\n")
		sb.WriteString("              echo 'ERROR: one or more project env-var POSTs failed (see above).' >&2\n")
		sb.WriteString("              exit 1\n")
		sb.WriteString("            fi\n")
		sb.WriteString(fmt.Sprintf("            echo 'Project env-var transfer complete for %q'\n", pp.SourceSlug))
		sb.WriteString("\n")
	}

	// Workflow: one job per context (and per project), all in parallel.
	sb.WriteString("workflows:\n")
	sb.WriteString("  transfer:\n")
	sb.WriteString("    jobs:\n")

	for _, cp := range ctxPlans {
		if len(cp.VarNames) == 0 {
			continue
		}
		jobName := transferJobName + "-" + sanitizeName(cp.SourceName)
		contexts := []string{cp.SourceName}
		if destTokenCtx != "" && destTokenCtx != cp.SourceName {
			contexts = append(contexts, destTokenCtx)
		}
		sb.WriteString("      - " + jobName + ":\n")
		sb.WriteString("          context:\n")
		for _, c := range contexts {
			sb.WriteString(fmt.Sprintf("            - %s\n", c))
		}
	}

	for _, pp := range projPlans {
		if pp.Skipped || len(pp.VarNames) == 0 {
			continue
		}
		jobName := projJobName + "-" + sanitizeName(pp.SourceSlug)
		// The job runs under the source project (env vars are injected); attach the
		// dest-token context so the job can authenticate to the destination API.
		sb.WriteString("      - " + jobName + ":\n")
		sb.WriteString("          context:\n")
		sb.WriteString(fmt.Sprintf("            - %s\n", destTokenCtx))
	}

	return sb.String()
}

// projectVarWorkerCount is the maximum number of concurrent per-project
// pipeline triggers and polls.  A value of 4 balances API rate-limit headroom
// against wall-clock time for large migrations.
const projectVarWorkerCount = 4

// buildSingleProjectTransferConfig builds an inline CircleCI YAML config that
// transfers env vars (and optionally additional SSH keys) for exactly ONE source
// project.  The generated pipeline runs under that project's own slug so CircleCI
// injects the project's env vars into the job environment.
//
// This is the core correctness fix for issue #263: project env vars are
// strictly project-scoped, so each project must run its own pipeline under its
// own slug — a single host-project pipeline cannot access another project's vars.
//
// When pp.SSHKeys is non-empty the generated config also:
//   - Adds an add_ssh_keys step that materialises the keys to ~/.ssh/id_rsa_<md5>.
//   - Runs a script that walks the materialised files, recomputes each key's SHA256
//     fingerprint (to match the manifest's catalogued fingerprints), reads the
//     private key VERBATIM via jq --rawfile (never echoed), and POSTs it to the
//     destination project via POST /api/v1.1/project/{slug}/ssh-key.
//
// SECURITY: private key material is read with jq --rawfile (no echo/cat to stdout).
// HTTP 201 and 200 are both treated as success (idempotent).
func buildSingleProjectTransferConfig(pp ProjectVarPlan, opts *Options) string {
	destHost := opts.destHost()
	destTokenEnvVar := opts.destTokenEnvVar()
	destTokenCtx := opts.DestTokenContext

	const projJobName = "circleci-migrate-transfer-project"

	var sb strings.Builder
	sb.WriteString("version: 2.1\n")
	sb.WriteString("jobs:\n")

	jobName := projJobName + "-" + sanitizeName(pp.SourceSlug)

	sb.WriteString("  " + jobName + ":\n")
	sb.WriteString("    docker:\n")
	sb.WriteString("      - image: cimg/base:current\n")
	sb.WriteString("    resource_class: small\n")
	sb.WriteString("    steps:\n")

	// ── SSH key materialisation (add_ssh_keys step) ────────────────────────────
	// When SSH keys are to be transferred, the add_ssh_keys step materialises
	// them to ~/.ssh/id_rsa_<md5fp> so the subsequent script can walk the files.
	if len(pp.SSHKeys) > 0 {
		sb.WriteString("      - add_ssh_keys:\n")
		sb.WriteString("          fingerprints:\n")
		for _, k := range pp.SSHKeys {
			sb.WriteString(fmt.Sprintf("            - \"SHA256:%s\"\n", k.Fingerprint))
		}
	}

	// ── Project env-var transfer step ─────────────────────────────────────────
	// The source project's env vars are available in the job environment
	// because the pipeline runs under that project's own slug (CircleCI
	// injects project-scoped env vars only when the pipeline belongs to that
	// exact project).  We POST each value to the destination project via the
	// v1.1 envvar API.
	//
	// CircleCI project env-var API:
	//   POST /api/v1.1/project/{slug}/envvar   → 201 (create or update)
	//   PUT  /api/v1.1/project/{slug}/envvar   → not available; POST is idempotent-upsert
	// We use POST (add-or-update) which returns 201 on create and 200 on update.
	if len(pp.VarNames) > 0 {
		sb.WriteString("      - run:\n")
		sb.WriteString(fmt.Sprintf("          name: Transfer project env vars for %q\n", pp.SourceSlug))
		sb.WriteString("          command: |\n")
		sb.WriteString("            set -euo pipefail\n")
		sb.WriteString("\n")
		sb.WriteString(fmt.Sprintf("            DEST_HOST=%q\n", destHost))
		sb.WriteString(fmt.Sprintf("            DEST_PROJECT_SLUG=%q\n", pp.DestSlug))
		sb.WriteString(fmt.Sprintf("            DEST_TOKEN=${%s:?%q env var is required (should be in the dest-token context)}\n",
			destTokenEnvVar, destTokenEnvVar))
		sb.WriteString("\n")
		sb.WriteString("            # POST each project env var to the destination project.\n")
		sb.WriteString("            # Values are available in the job environment from the source project\n")
		sb.WriteString("            # because this pipeline runs under that project's own slug.\n")
		sb.WriteString("            transfer_ok=true\n")
		for _, varName := range pp.VarNames {
			safeVar := strings.ReplaceAll(varName, "'", "'\\''")
			sb.WriteString(fmt.Sprintf("            # Transfer project var %s\n", varName))
			sb.WriteString(fmt.Sprintf("            val=${%s:-}\n", safeVar))
			sb.WriteString("            body=$(jq -n --arg n \"" + varName + "\" --arg v \"$val\" '{\"name\": $n, \"value\": $v}')\n")
			sb.WriteString("            http_code=$(curl -s -o /dev/null -w '%{http_code}' \\\n")
			sb.WriteString("              -X POST \\\n")
			sb.WriteString("              -H 'Content-Type: application/json' \\\n")
			sb.WriteString("              -H \"Circle-Token: ${DEST_TOKEN}\" \\\n")
			sb.WriteString("              -d \"$body\" \\\n")
			sb.WriteString("              \"${DEST_HOST}/api/v1.1/project/${DEST_PROJECT_SLUG}/envvar\")\n")
			sb.WriteString("            if [ \"$http_code\" != '201' ] && [ \"$http_code\" != '200' ]; then\n")
			sb.WriteString(fmt.Sprintf("              echo \"ERROR: POST project var %s HTTP ${http_code}\" >&2\n", varName))
			sb.WriteString("              transfer_ok=false\n")
			sb.WriteString("            else\n")
			sb.WriteString(fmt.Sprintf("              echo \"Transferred project var: %s\"\n", varName))
			sb.WriteString("            fi\n")
		}
		sb.WriteString("            if [ \"$transfer_ok\" = 'false' ]; then\n")
		sb.WriteString("              echo 'ERROR: one or more project env-var POSTs failed (see above).' >&2\n")
		sb.WriteString("              exit 1\n")
		sb.WriteString("            fi\n")
		sb.WriteString(fmt.Sprintf("            echo 'Project env-var transfer complete for %q'\n", pp.SourceSlug))
		sb.WriteString("\n")
	}

	// ── SSH key transfer step ──────────────────────────────────────────────────
	// Walks the materialised key files from the add_ssh_keys step above, matches
	// each file by recomputing its SHA256 fingerprint, reads the private key
	// material VERBATIM with jq --rawfile (never echoed), and POSTs it to the
	// destination project.
	//
	// POST /api/v1.1/project/{slug}/ssh-key
	//   Body: {"hostname": "<host>", "private_key": "<pem>"}
	//   Success: 201 or 200 (idempotent — already-present key is a no-op).
	//
	// SECURITY:
	//   - Keys are never echoed; jq --rawfile captures file contents without
	//     shell interpolation or command substitution (which strips trailing
	//     newlines, producing invalid keys).
	//   - curl -s: silent; no response body printed.
	//   - The dest token is referenced only as ${CIRCLECI_DEST_TOKEN}.
	if len(pp.SSHKeys) > 0 {
		sb.WriteString("      - run:\n")
		sb.WriteString(fmt.Sprintf("          name: Transfer additional SSH keys for %q\n", pp.SourceSlug))
		sb.WriteString("          command: |\n")
		sb.WriteString("            set -euo pipefail\n")
		sb.WriteString("\n")
		sb.WriteString(fmt.Sprintf("            DEST_HOST=%q\n", destHost))
		sb.WriteString(fmt.Sprintf("            DEST_PROJECT_SLUG=%q\n", pp.DestSlug))
		// #nosec G101 -- DEST_TOKEN_VAR is the NAME of the env var, not a credential
		sb.WriteString(fmt.Sprintf("            DEST_TOKEN=${%s:?%q env var is required (should be in the dest-token context)}\n",
			destTokenEnvVar, destTokenEnvVar))
		sb.WriteString("\n")
		sb.WriteString("            # fingerprint (bare SHA256) → hostname lookup from manifest catalog.\n")
		sb.WriteString("            # Keys are base64+= chars; hostnames are hostname-safe — no shell-specials.\n")
		sb.WriteString("            declare -A FP_TO_HOST\n")
		for _, k := range pp.SSHKeys {
			sb.WriteString(fmt.Sprintf("            FP_TO_HOST[%q]=%q\n", k.Fingerprint, k.Hostname))
		}
		sb.WriteString("\n")
		sb.WriteString("            transfer_ok=true\n")
		sb.WriteString(`            for f in "$HOME"/.ssh/id_rsa_* "$HOME"/.ssh/id_ed25519_* "$HOME"/.ssh/id_ecdsa_*; do
              [ -f "$f" ] || continue
              # Recompute SHA256 fingerprint: "2048 SHA256:<fp> comment (RSA)"
              fp_line=$(ssh-keygen -lf "$f" -E sha256 2>/dev/null) || continue
              # Extract bare fingerprint after "SHA256:"
              fp=$(echo "$fp_line" | grep -oP '(?<=SHA256:)[A-Za-z0-9+/=]+') || continue
              # Skip if not in our catalog
              if [ -z "${FP_TO_HOST[$fp]+set}" ]; then
                continue
              fi
              host="${FP_TO_HOST[$fp]}"
              # Read private key VERBATIM with jq --rawfile; never echo key material.
              # --rawfile preserves the trailing newline (command substitution strips it,
              # producing an invalid OpenSSH key that CircleCI would reject).
              body=$(jq -n --arg hn "$host" --rawfile pk "$f" '{"hostname":$hn,"private_key":$pk}')
              http_code=$(curl -s -o /dev/null -w '%{http_code}' \
                -X POST \
                -H 'Content-Type: application/json' \
                -H "Circle-Token: ${DEST_TOKEN}" \
                -d "$body" \
                "${DEST_HOST}/api/v1.1/project/${DEST_PROJECT_SLUG}/ssh-key")
              if [ "$http_code" != '201' ] && [ "$http_code" != '200' ]; then
                echo "ERROR: POST ssh-key fp=${fp} host=${host} HTTP ${http_code}" >&2
                transfer_ok=false
              else
                echo "Transferred SSH key: fp=${fp} host=${host}"
              fi
            done
`)
		sb.WriteString("            if [ \"$transfer_ok\" = 'false' ]; then\n")
		sb.WriteString("              echo 'ERROR: one or more SSH key POSTs failed (see above).' >&2\n")
		sb.WriteString("              exit 1\n")
		sb.WriteString("            fi\n")
		sb.WriteString(fmt.Sprintf("            echo 'SSH key transfer complete for %q'\n", pp.SourceSlug))
		sb.WriteString("\n")
	}

	// Workflow: single job, attached only to the dest-token context.
	sb.WriteString("workflows:\n")
	sb.WriteString("  transfer:\n")
	sb.WriteString("    jobs:\n")
	sb.WriteString("      - " + jobName + ":\n")
	sb.WriteString("          context:\n")
	sb.WriteString(fmt.Sprintf("            - %s\n", destTokenCtx))

	return sb.String()
}

// projectPipelineResult records the outcome of a single per-project pipeline.
type projectPipelineResult struct {
	sourceSlug string
	err        error // nil on success
}

// lockedWriter serializes concurrent writes to an underlying io.Writer.  The
// per-project transfer goroutines share a single progress writer (which may be
// a bytes.Buffer or os.Stderr — neither safe for concurrent use), so all of
// their writes are funneled through this mutex.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// runProjectVarPipelines triggers one pipeline per non-skipped ProjectVarPlan,
// each under that project's own slug, and polls them concurrently with a
// bounded worker pool.  It returns a slice of results (one per active plan) and
// a combined error if any project pipeline failed.
//
// Correctness invariant: every pipeline runs under pp.SourceSlug so CircleCI
// injects THAT project's env vars into the job.  Using a shared host project
// would give only the host project's vars, corrupting all other projects.
func runProjectVarPipelines(ctx context.Context, deps Deps, activePlans []ProjectVarPlan, opts *Options, errOut io.Writer) ([]projectPipelineResult, error) {
	results := make([]projectPipelineResult, len(activePlans))
	sem := make(chan struct{}, projectVarWorkerCount)

	// The worker goroutines below run concurrently and all log progress to
	// errOut. io.Writer implementations such as bytes.Buffer and os.Stderr are
	// not safe for concurrent writes, so funnel every goroutine's output
	// through a single mutex-guarded writer to avoid a data race.
	safeOut := &lockedWriter{w: errOut}

	var wg sync.WaitGroup
	for i, pp := range activePlans {
		wg.Add(1)
		go func(idx int, plan ProjectVarPlan) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			results[idx] = projectPipelineResult{
				sourceSlug: plan.SourceSlug,
				err:        triggerAndPollProjectPipeline(ctx, deps, plan, opts, safeOut),
			}
		}(i, pp)
	}
	wg.Wait()

	// Collect errors; attempt all projects before returning.
	var errs []string
	for _, r := range results {
		if r.err != nil {
			errs = append(errs, fmt.Sprintf("project %q: %v", r.sourceSlug, r.err))
		}
	}
	if len(errs) > 0 {
		return results, fmt.Errorf("transfer: %d project pipeline(s) failed:\n  %s",
			len(errs), strings.Join(errs, "\n  "))
	}
	return results, nil
}

// triggerAndPollProjectPipeline triggers one inline pipeline for a single
// project and polls it to terminal state.  It resolves the project and its
// pipeline definition, builds a single-project config, triggers the run, and
// waits for the workflow to complete.
//
// When the workflow returns "unauthorized" the trigger+poll is automatically
// retried up to unauthorizedRetryMax times with a unauthorizedRetryDelay delay.
// This covers the common case where a freshly-followed project's context
// authorization has not yet propagated.
func triggerAndPollProjectPipeline(ctx context.Context, deps Deps, pp ProjectVarPlan, opts *Options, errOut io.Writer) error {
	proj, err := deps.GetProject(ctx, pp.SourceSlug)
	if err != nil {
		return fmt.Errorf("get project: %w", err)
	}

	defs, err := deps.ListPipelineDefinitions(ctx, proj.ID)
	if err != nil {
		return fmt.Errorf("list pipeline definitions: %w", err)
	}
	if len(defs) == 0 {
		return fmt.Errorf("project %s has no pipeline definitions — is the repo connected to a GitHub App?", pp.SourceSlug)
	}
	defID := defs[0].ID

	configYAML := buildSingleProjectTransferConfigWithVersion(pp, opts)

	// retryDelay is unauthorizedRetryDelay unless the caller has set a custom
	// PollInterval (used by tests to avoid real sleeps).
	retryDelay := opts.pollInterval()
	if retryDelay > unauthorizedRetryDelay {
		retryDelay = unauthorizedRetryDelay
	}

	for attempt := 0; attempt <= unauthorizedRetryMax; attempt++ {
		if attempt > 0 {
			fmt.Fprintf(errOut,
				"  [project vars] workflow unauthorized — the host project may not be permitted to use a restricted context (or context authorization is still propagating); retrying in %s… (attempt %d/%d)\n",
				retryDelay, attempt, unauthorizedRetryMax)
			select {
			case <-ctx.Done():
				return fmt.Errorf("context cancelled while waiting to retry unauthorized pipeline: %w", ctx.Err())
			case <-time.After(retryDelay):
			}
		}

		fmt.Fprintf(errOut, "  [project vars] triggering pipeline under %s (definition %s)…\n", pp.SourceSlug, defID)

		pipelineID, trigErr := deps.TriggerPipelineRun(ctx, pp.SourceSlug, defID, opts.branchFor(pp.DefaultBranch), configYAML, nil)
		if trigErr != nil {
			if errors.Is(trigErr, project.ErrPipelineSkipped) {
				return fmt.Errorf("pipeline run was skipped — check api-trigger-with-config is enabled")
			}
			return fmt.Errorf("trigger pipeline: %w", trigErr)
		}

		fmt.Fprintf(errOut, "  [project vars] pipeline triggered for %s: %s\n", pp.SourceSlug, pipelineID)

		pollCtx := ctx
		var cancel context.CancelFunc
		if opts.PollTimeout > 0 {
			pollCtx, cancel = context.WithTimeout(ctx, opts.PollTimeout)
		}

		wf, pollErr := pollWorkflow(pollCtx, deps, pipelineID, opts.pollInterval(), errOut)
		if cancel != nil {
			cancel()
		}
		if pollErr != nil {
			return fmt.Errorf("poll: %w", pollErr)
		}
		if wf.Status == "unauthorized" {
			if attempt < unauthorizedRetryMax {
				// retry in the next loop iteration
				continue
			}
			return fmt.Errorf("%w: status=%q workflow=%q — workflow unauthorized after %d retries: the host project may not be permitted to use a restricted context, or context authorization has not yet propagated; if a context has project/expression restrictions, re-run with --remove-restrictions, or use --host-project to a project that the context allows",
				ErrWorkflowFailed, wf.Status, wf.Name, unauthorizedRetryMax)
		}
		if wf.Status != "success" {
			return fmt.Errorf("%w: status=%q workflow=%q", ErrWorkflowFailed, wf.Status, wf.Name)
		}
		return nil
	}
	// unreachable (loop always returns), but satisfies the compiler.
	return fmt.Errorf("%w: exhausted retries", ErrWorkflowFailed)
}

// buildSingleProjectTransferConfigWithVersion is the testable variant of
// buildSingleProjectTransferConfig (kept for test backward-compat).
func buildSingleProjectTransferConfigWithVersion(pp ProjectVarPlan, opts *Options) string {
	return buildSingleProjectTransferConfig(pp, opts)
}

// sanitizeName converts a context name to a safe job-name suffix.
// Replaces characters that are not alphanumeric or hyphen with hyphens and
// trims leading/trailing hyphens.
func sanitizeName(name string) string {
	var sb strings.Builder
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') {
			sb.WriteRune(ch)
		} else {
			sb.WriteRune('-')
		}
	}
	result := sb.String()
	// Trim leading/trailing hyphens.
	result = strings.Trim(result, "-")
	if result == "" {
		return "ctx"
	}
	// Lowercase for consistency.
	return strings.ToLower(result)
}

// ─────────────────────────────────────────────────────────────────────────────
// Restriction helpers
// ─────────────────────────────────────────────────────────────────────────────

// handleContextRestrictions inspects the plan for contexts with blocking
// restrictions (project or expression type).  When --remove-restrictions is set
// it temporarily deletes those restrictions via the source-org context API and
// registers a deferred restore.  When it is not set it returns an actionable
// error listing the blocking contexts.
//
// IMPORTANT: group restrictions (including the default "All members" group) are
// NEVER removed — they are org-type specific and cannot always be recreated via
// API.  Only project and expression restrictions are touched.
//
// It returns a cleanup func that the CALLER must defer — the removed
// restrictions must stay lifted across the ENTIRE context-pipeline run, so the
// restore cannot happen when this function returns. cleanup is always non-nil
// (a no-op when nothing was removed) and is safe to call once.
func handleContextRestrictions(ctx context.Context, m *manifest.Manifest, plan *Plan, opts *Options) (func(), error) {
	noop := func() {}
	// Collect contexts with blocking restrictions from the plan.
	type blockedCtx struct {
		ctxPlan    *ContextPlan
		mc         *manifest.Context
		numBlocked int
	}

	var blocked []blockedCtx
	for i := range plan.Contexts {
		cp := &plan.Contexts[i]
		if len(cp.BlockingRestrictions) == 0 {
			continue
		}
		// Locate the manifest context to get the SourceID for the API call.
		var mc *manifest.Context
		for j := range m.Contexts {
			if m.Contexts[j].Name == cp.SourceName {
				mc = &m.Contexts[j]
				break
			}
		}
		blocked = append(blocked, blockedCtx{ctxPlan: cp, mc: mc, numBlocked: len(cp.BlockingRestrictions)})
	}

	if len(blocked) == 0 {
		return noop, nil // no blocking restrictions
	}

	if !opts.RemoveRestrictions {
		// Fail fast with an actionable message.
		var ctxNames []string
		for _, b := range blocked {
			types := make([]string, 0, len(b.ctxPlan.BlockingRestrictions))
			for _, r := range b.ctxPlan.BlockingRestrictions {
				types = append(types, r.Type)
			}
			ctxNames = append(ctxNames,
				fmt.Sprintf("%q (%s restriction(s))", b.ctxPlan.SourceName, strings.Join(types, ",")))
		}
		hostSlug := opts.HostProjectSlug
		if hostSlug == "" && len(m.Projects) > 0 {
			hostSlug = m.Projects[0].Slug
		}
		return noop, fmt.Errorf(
			"transfer: %d context(s) have project/expression restriction(s) that prevent the transfer "+
				"pipeline from reading them under host project %q:\n  %s\n"+
				"Re-run with --remove-restrictions (temporarily lifts the restriction on the source and "+
				"restores it after), or pass --host-project to a project that the context allows",
			len(blocked), hostSlug, strings.Join(ctxNames, "\n  "))
	}

	// --remove-restrictions: validate that a ContextClient is wired.
	if opts.ContextClient == nil {
		return noop, fmt.Errorf("transfer: --remove-restrictions requires a context API client (ContextClient must be set in Options)")
	}

	// Remove blocking restrictions and collect restore closures. The caller
	// defers the returned cleanup so the restrictions stay lifted across the
	// ENTIRE context-pipeline run, then are restored afterwards.
	var restores []func()
	cleanup := func() {
		for _, r := range restores {
			r()
		}
	}
	for _, b := range blocked {
		if b.mc == nil || b.mc.SourceID == "" {
			fmt.Fprintf(opts.Stderr,
				"WARNING: context %q has blocking restrictions but no source_id in manifest — cannot remove/restore; skipping restriction removal for this context.\n",
				b.ctxPlan.SourceName)
			continue
		}
		restore, err := prepareTransferRestrictionRemoval(ctx, opts.Stderr, opts.ContextClient, b.mc)
		if err != nil {
			// Restore anything already removed before surfacing the error.
			cleanup()
			return noop, fmt.Errorf("transfer: preparing restriction removal for context %q: %w", b.ctxPlan.SourceName, err)
		}
		if restore != nil {
			restores = append(restores, restore)
		}
	}

	return cleanup, nil
}

// prepareTransferRestrictionRemoval fetches live restriction IDs for mc,
// deletes only project and expression restrictions (skipping ALL group
// restrictions), and returns a restore function that re-creates them.
//
// This mirrors internal/capture.prepareRestrictionRemoval but operates on the
// transfer path — no manifest.SecretBundle or capture-specific types needed.
func prepareTransferRestrictionRemoval(ctx context.Context, stderr io.Writer, client ContextRestrictionManager, mc *manifest.Context) (restoreFn func(), err error) {
	// Fetch live restrictions to get their IDs for deletion.
	live, listErr := client.ListRestrictions(ctx, mc.SourceID)
	if listErr != nil {
		return func() {}, fmt.Errorf("listing live restrictions for context %q: %w", mc.Name, listErr)
	}

	// Filter live restrictions: only touch project and expression types.
	// ALL group restrictions (including the default "All members" group) are
	// left completely untouched.
	var liveToDelete []apicontext.Restriction
	for _, lr := range live {
		if lr.Type == "group" {
			fmt.Fprintf(stderr,
				"NOTICE: group restriction on context %q (value=%q) is managed by CircleCI/VCS and is not modified.\n",
				mc.Name, lr.Value)
			continue
		}
		liveToDelete = append(liveToDelete, lr)
	}

	// The restore set comes from the manifest's recorded restrictions, filtered
	// to only project and expression types.
	var restoreFrom []manifest.Restriction
	for _, r := range mc.Restrictions {
		if r.Type == "group" {
			continue // never remove or restore group restrictions
		}
		restoreFrom = append(restoreFrom, r)
	}

	fmt.Fprintf(stderr,
		"NOTICE: temporarily removing %d project/expression restriction(s) from context %q for transfer.\n",
		len(liveToDelete), mc.Name)
	for _, lr := range liveToDelete {
		if delErr := client.DeleteRestriction(ctx, mc.SourceID, lr.ID); delErr != nil {
			return func() {}, fmt.Errorf("deleting restriction %q from context %q: %w", lr.ID, mc.Name, delErr)
		}
	}

	restore := func() {
		fmt.Fprintf(stderr,
			"NOTICE: restoring %d project/expression restriction(s) on context %q.\n",
			len(restoreFrom), mc.Name)
		for _, r := range restoreFrom {
			if createErr := client.CreateRestriction(ctx, mc.SourceID, r.Type, r.Value); createErr != nil {
				fmt.Fprintf(stderr,
					"WARNING: failed to restore restriction on context %q "+
						"(type=%q value=%q): %v — you must re-add this restriction manually.\n",
					mc.Name, r.Type, r.Value, createErr)
			}
		}
	}
	return restore, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Plan builder
// ─────────────────────────────────────────────────────────────────────────────

// BuildPlan resolves which contexts and variables would be transferred given
// the manifest and options. It does NOT trigger any pipelines.
func BuildPlan(m *manifest.Manifest, opts *Options) (Plan, error) {
	if opts.DestOrgID == "" {
		return Plan{}, errors.New("transfer: --dest-org-id is required")
	}
	if opts.DestTokenContext == "" {
		return Plan{}, errors.New("transfer: --dest-token-context is required (name of the source context holding the dest API token)")
	}

	var ctxPlans []ContextPlan
	for _, mc := range m.Contexts {
		if len(opts.SelectedContextNames) > 0 && !opts.SelectedContextNames[mc.Name] {
			continue
		}
		if len(mc.EnvVars) == 0 {
			continue
		}

		varNames := make([]string, 0, len(mc.EnvVars))
		for _, ev := range mc.EnvVars {
			varNames = append(varNames, ev.Name)
		}
		sort.Strings(varNames)

		// Detect blocking restrictions from the manifest.  The default
		// "All members" group restriction (type=="group", value==orgID) is not a
		// real restriction — skip it.  Non-default group restrictions are also
		// excluded from the blocking list because they are not removable via API
		// on all org types (project and expression restrictions are removable on
		// all org types).
		var blocking []manifest.Restriction
		for _, r := range mc.Restrictions {
			if r.Type == "group" {
				continue // never treat any group restriction as blocking
			}
			blocking = append(blocking, r)
		}

		ctxPlans = append(ctxPlans, ContextPlan{
			SourceName: mc.Name,
			DestName:   opts.destContextName(mc.Name),
			VarNames:   varNames,
			// WillCreate is always false at plan time — whether the context exists
			// in the destination is unknown without a live API call.  The
			// in-pipeline job handles create-if-missing; the plan shows the intent.
			WillCreate:           false,
			BlockingRestrictions: blocking,
		})
	}

	if len(ctxPlans) == 0 && !opts.IncludeProjectVars && !opts.IncludeSSHKeys {
		return Plan{}, errors.New("transfer: no contexts with env-var values found in manifest (nothing to transfer)")
	}

	// Per-project plans: merge env-var and SSH-key data into a single
	// ProjectVarPlan per source project.  A project that can be resolved to a
	// dest slug gets one plan entry; an unresolvable project is skipped.
	//
	// We use a map keyed by source slug to accumulate both env vars and SSH keys
	// into a single plan entry, then append to projPlans in stable manifest order.
	type partialPlan struct {
		destSlug      string
		varNames      []string
		sshKeys       []SSHKeyPlan
		defaultBranch string
		skipped       bool
		skipMsg       string
	}
	partial := make(map[string]*partialPlan)
	var projOrder []string // stable source-slug order

	ensureEntry := func(srcSlug string) *partialPlan {
		if _, ok := partial[srcSlug]; !ok {
			partial[srcSlug] = &partialPlan{}
			projOrder = append(projOrder, srcSlug)
		}
		return partial[srcSlug]
	}

	// ── Project env-var contribution ─────────────────────────────────────────
	if opts.IncludeProjectVars {
		for _, mp := range m.Projects {
			if len(mp.EnvVars) == 0 {
				continue
			}
			p := ensureEntry(mp.Slug)
			p.defaultBranch = mp.VCS.DefaultBranch
			destSlug, ok := opts.destProjectSlug(mp.Slug)
			if !ok {
				p.skipped = true
				p.skipMsg = fmt.Sprintf("dest project for %q unknown — provide --mapping or onboard it first; skipped", mp.Slug)
				continue
			}
			p.destSlug = destSlug
			varNames := make([]string, 0, len(mp.EnvVars))
			for _, ev := range mp.EnvVars {
				varNames = append(varNames, ev.Name)
			}
			sort.Strings(varNames)
			p.varNames = varNames
		}
	}

	// ── SSH-key contribution ───────────────────────────────────────────────────
	if opts.IncludeSSHKeys {
		for _, mp := range m.Projects {
			if len(mp.SSHKeys) == 0 {
				continue
			}
			p := ensureEntry(mp.Slug)
			p.defaultBranch = mp.VCS.DefaultBranch
			// If already skipped due to an unresolvable dest slug, keep it skipped.
			if p.skipped {
				continue
			}
			if p.destSlug == "" {
				destSlug, ok := opts.destProjectSlug(mp.Slug)
				if !ok {
					p.skipped = true
					p.skipMsg = fmt.Sprintf("dest project for %q unknown — provide --mapping or onboard it first; skipped", mp.Slug)
					continue
				}
				p.destSlug = destSlug
			}
			keys := make([]SSHKeyPlan, 0, len(mp.SSHKeys))
			for _, k := range mp.SSHKeys {
				keys = append(keys, SSHKeyPlan{
					Fingerprint: k.Fingerprint,
					Hostname:    k.Hostname,
				})
			}
			p.sshKeys = keys
		}
	}

	// Build the final ordered projPlans slice.
	var projPlans []ProjectVarPlan
	for _, slug := range projOrder {
		p := partial[slug]
		if p.skipped {
			projPlans = append(projPlans, ProjectVarPlan{
				SourceSlug:    slug,
				DefaultBranch: p.defaultBranch,
				Skipped:       true,
				SkipReason:    p.skipMsg,
			})
		} else {
			projPlans = append(projPlans, ProjectVarPlan{
				SourceSlug:    slug,
				DestSlug:      p.destSlug,
				VarNames:      p.varNames,
				SSHKeys:       p.sshKeys,
				DefaultBranch: p.defaultBranch,
			})
		}
	}

	if len(ctxPlans) == 0 && len(projPlans) == 0 {
		return Plan{}, errors.New("transfer: no contexts or projects with transferable data found in manifest (nothing to transfer)")
	}
	// If only project-var/SSH-key mode and ALL projects are skipped, that's a
	// usable but warning-worthy plan — we return it; the caller sees the SKIP lines.

	return Plan{
		Contexts:         ctxPlans,
		Projects:         projPlans,
		DestTokenContext: opts.DestTokenContext,
		DestTokenEnvVar:  opts.destTokenEnvVar(),
	}, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Transfer orchestrator
// ─────────────────────────────────────────────────────────────────────────────

// terminalStatuses is the set of CircleCI workflow statuses that indicate the
// pipeline has finished (success, failure, or cancellation).
//
// "unauthorized" is included so that a freshly-followed project whose context
// authorization hasn't propagated yet stops the poll rather than hanging until
// --poll-timeout. The caller (triggerAndPollProjectPipeline) recognises this
// status and retries the full trigger+poll sequence automatically.
//
// "not_run" is included for workflows that were never executed (e.g. the
// pipeline was blocked by a branch filter) — polling indefinitely for a
// workflow that will never run is never correct.
var terminalStatuses = map[string]bool{
	"success":      true,
	"failed":       true,
	"error":        true,
	"canceled":     true,
	"unauthorized": true,
	"not_run":      true,
}

// unauthorizedRetryMax is the maximum number of times a per-project pipeline
// trigger is automatically retried when the workflow returns "unauthorized".
// Freshly-followed projects can take a minute or two for context authorization
// to propagate; two retries covers the typical propagation window.
const unauthorizedRetryMax = 2

// unauthorizedRetryDelay is the wait between automatic retries when
// "unauthorized" is returned. Callers can override via Options.PollInterval for
// tests (the retry delay is always the poll interval value).
const unauthorizedRetryDelay = 30 * time.Second

// ErrWorkflowFailed is returned when the transfer workflow finishes in a
// non-success terminal state.
var ErrWorkflowFailed = errors.New("transfer workflow did not succeed")

// Transfer builds the plan and, when opts.DryRun is false, triggers the
// transfer pipeline(s) and waits for them to complete.
//
// Context transfer: one pipeline under the host project carries all context
// jobs (contexts are org-scoped, so any host project works).
//
// Project env-var transfer (--include-project-vars): one pipeline per source
// project, triggered under that project's own slug, run concurrently with a
// bounded worker pool (projectVarWorkerCount).  This is the correct approach
// because CircleCI project env vars are strictly project-scoped — a pipeline
// running under a different project cannot access them.
//
// When opts.DryRun is true (the default), Transfer only prints the plan to
// opts.Stdout and opts.Stderr — no pipeline is triggered.
//
// SECURITY: no secret values are logged or returned.
func Transfer(ctx context.Context, deps Deps, m *manifest.Manifest, opts Options) error {
	plan, err := BuildPlan(m, &opts)
	if err != nil {
		return err
	}

	// Always print the plan.
	printPlan(opts.Stdout, opts.Stderr, &plan, &opts)

	if opts.DryRun {
		fmt.Fprintln(opts.Stdout, "\nDry-run mode: no pipeline triggered. Pass --apply to execute the transfer.")
		return nil
	}

	// ── Restriction pre-check / removal ──────────────────────────────────────
	// Contexts with project or expression restrictions will cause the transfer
	// pipeline to come back "unauthorized" if the host project is not in the
	// allowed set.  Detect this before triggering so the operator gets an
	// actionable error rather than a misleading "unauthorized" retry loop.
	restrictionCleanup, err := handleContextRestrictions(ctx, m, &plan, &opts)
	if err != nil {
		return err
	}
	// Keep restrictions lifted across BOTH the context and per-project pipelines,
	// then restore. (Must outlive runContextPipeline — restoring earlier would
	// re-block the context pipeline and cause an 'unauthorized' workflow.)
	defer restrictionCleanup()

	// ── Phase 1: context transfer on host project ─────────────────────────────

	var ctxErr error
	if len(plan.Contexts) > 0 {
		ctxErr = runContextPipeline(ctx, deps, m, &plan, &opts)
	}

	// ── Phase 2: per-project pipeline for project env vars and/or SSH keys ──────

	// Collect non-skipped project plans that have something to transfer: env vars
	// (when IncludeProjectVars is set) and/or SSH keys (when IncludeSSHKeys is set).
	// A project with only SSH keys still needs its own pipeline even if VarNames
	// is empty.
	var activeProjPlans []ProjectVarPlan
	for _, pp := range plan.Projects {
		if !pp.Skipped && (len(pp.VarNames) > 0 || len(pp.SSHKeys) > 0) {
			activeProjPlans = append(activeProjPlans, pp)
		}
	}

	var projResults []projectPipelineResult
	var projErr error
	if len(activeProjPlans) > 0 {
		if len(activeProjPlans) > 1 {
			fmt.Fprintf(opts.Stderr, "Triggering %d per-project pipeline(s) for project env-var/SSH-key transfer (concurrency: %d)…\n",
				len(activeProjPlans), projectVarWorkerCount)
		} else {
			fmt.Fprintf(opts.Stderr, "Triggering project transfer pipeline for %s…\n",
				activeProjPlans[0].SourceSlug)
		}
		projResults, projErr = runProjectVarPipelines(ctx, deps, activeProjPlans, &opts, opts.Stderr)
	}

	// ── Summary ───────────────────────────────────────────────────────────────

	if ctxErr == nil && len(plan.Contexts) > 0 {
		fmt.Fprintf(opts.Stdout, "\nContext transfer pipeline succeeded: %d context(s), %d context variable(s).\n",
			len(plan.Contexts), plan.TotalVars())
	}

	if len(activeProjPlans) > 0 {
		successes := 0
		for _, r := range projResults {
			if r.err == nil {
				successes++
			}
		}
		fmt.Fprintf(opts.Stdout, "Project transfer: %d/%d project pipeline(s) succeeded (%d project variable(s), %d SSH key(s) targeted).\n",
			successes, len(activeProjPlans), plan.TotalProjectVars(), plan.TotalSSHKeys())
	}

	// Return the first non-nil error (context error takes precedence).
	if ctxErr != nil {
		return ctxErr
	}
	return projErr
}

// runContextPipeline triggers and polls the single host-project pipeline that
// carries all context transfer jobs.
func runContextPipeline(ctx context.Context, deps Deps, m *manifest.Manifest, plan *Plan, opts *Options) error {
	if opts.HostProjectSlug == "" {
		if len(m.Projects) == 0 {
			return errors.New("transfer: a host project is required; pass --host-project or ensure the manifest contains projects")
		}
		opts.HostProjectSlug = m.Projects[0].Slug
		fmt.Fprintf(opts.Stderr, "Auto-picked host project %s for transfer pipeline (use --host-project to override).\n", opts.HostProjectSlug)
	}

	proj, err := deps.GetProject(ctx, opts.HostProjectSlug)
	if err != nil {
		return fmt.Errorf("transfer: get project %s: %w", opts.HostProjectSlug, err)
	}

	defs, err := deps.ListPipelineDefinitions(ctx, proj.ID)
	if err != nil {
		return fmt.Errorf("transfer: list pipeline definitions for %s: %w", opts.HostProjectSlug, err)
	}
	if len(defs) == 0 {
		return fmt.Errorf("transfer: project %s has no pipeline definitions — is the repo connected to a GitHub App?", opts.HostProjectSlug)
	}
	defID := defs[0].ID

	// Build context-only config (pass nil project plans — project vars are
	// handled separately by per-project pipelines).
	configYAML := buildTransferConfig(m, plan.Contexts, nil, opts)

	// The context pipeline runs under the (often auto-picked) host project. When
	// that project was just followed (e.g. the one-command migrate flow follows
	// projects then immediately transfers), its context authorization may not
	// have propagated yet → workflow status "unauthorized". Retry the trigger a
	// few times, mirroring the per-project path.
	retryDelay := opts.pollInterval()
	if retryDelay > unauthorizedRetryDelay {
		retryDelay = unauthorizedRetryDelay
	}

	for attempt := 0; attempt <= unauthorizedRetryMax; attempt++ {
		if attempt > 0 {
			fmt.Fprintf(opts.Stderr,
				"Context pipeline unauthorized — the host project may not be permitted to use a restricted context (or context authorization is still propagating); retrying in %s… (attempt %d/%d)\n",
				retryDelay, attempt, unauthorizedRetryMax)
			select {
			case <-ctx.Done():
				return fmt.Errorf("transfer: context cancelled while waiting to retry unauthorized context pipeline: %w", ctx.Err())
			case <-time.After(retryDelay):
			}
		}

		fmt.Fprintf(opts.Stderr, "Triggering context transfer pipeline under %s (definition %s)…\n", opts.HostProjectSlug, defID)

		pipelineID, trigErr := deps.TriggerPipelineRun(ctx, opts.HostProjectSlug, defID, opts.branchFor(hostDefaultBranch(m, opts.HostProjectSlug)), configYAML, nil)
		if trigErr != nil {
			if errors.Is(trigErr, project.ErrPipelineSkipped) {
				return fmt.Errorf("transfer: pipeline run was skipped — check api-trigger-with-config is enabled and the config is valid")
			}
			return fmt.Errorf("transfer: trigger pipeline: %w", trigErr)
		}
		fmt.Fprintf(opts.Stderr, "Context pipeline triggered: %s\n", pipelineID)

		pollCtx := ctx
		var cancel context.CancelFunc
		if opts.PollTimeout > 0 {
			pollCtx, cancel = context.WithTimeout(ctx, opts.PollTimeout)
		}
		wf, pollErr := pollWorkflow(pollCtx, deps, pipelineID, opts.pollInterval(), opts.Stderr)
		if cancel != nil {
			cancel()
		}
		if pollErr != nil {
			return fmt.Errorf("transfer: poll: %w", pollErr)
		}
		if wf.Status == "unauthorized" {
			if attempt < unauthorizedRetryMax {
				continue
			}
			return fmt.Errorf("%w: status=%q workflow=%q — workflow unauthorized after %d retries: the host project may not be permitted to use a restricted context, or context authorization has not yet propagated; if a context has project/expression restrictions, re-run with --remove-restrictions, or use --host-project to a project that the context allows",
				ErrWorkflowFailed, wf.Status, wf.Name, unauthorizedRetryMax)
		}
		if wf.Status != "success" {
			return fmt.Errorf("%w: status=%q workflow=%q", ErrWorkflowFailed, wf.Status, wf.Name)
		}
		return nil
	}
	return fmt.Errorf("%w: exhausted retries", ErrWorkflowFailed)
}

// hostDefaultBranch returns the recorded default branch (vcs.default_branch)
// for the host project slug from the manifest, or "" when the slug is not
// present or has no recorded default branch. The caller passes the result to
// Options.branchFor so an explicit --branch still overrides it and a missing
// value falls back to "main".
func hostDefaultBranch(m *manifest.Manifest, hostSlug string) string {
	for i := range m.Projects {
		if m.Projects[i].Slug == hostSlug {
			return m.Projects[i].VCS.DefaultBranch
		}
	}
	return ""
}

// printPlan writes the transfer plan to stdout/stderr so operators can review
// what would happen before committing to --apply.
func printPlan(out, errOut io.Writer, plan *Plan, opts *Options) {
	ren := ui.New(out)
	errRen := ui.New(errOut)

	// ── Header on stderr ──────────────────────────────────────────────────────
	errRen.Section("Transfer plan", "")
	fmt.Fprintf(errOut, "  Dest token: context=%q env-var=%q\n", plan.DestTokenContext, plan.DestTokenEnvVar)
	fmt.Fprintf(errOut, "  Dest org ID: %s\n", opts.DestOrgID)
	fmt.Fprintf(errOut, "  Dest host: %s\n", opts.destHost())
	fmt.Fprintln(errOut, "")

	// ── Contexts section ──────────────────────────────────────────────────────
	if len(plan.Contexts) > 0 {
		ren.Section("Contexts", "one pipeline, one job per context — jobs run in parallel")
		for _, cp := range plan.Contexts {
			action := "update"
			// The in-pipeline job performs create-if-missing automatically; we label
			// the action accordingly in the plan to set operator expectations.
			if cp.WillCreate {
				action = "create"
			}
			var label string
			if cp.SourceName == cp.DestName {
				label = fmt.Sprintf("context %q [%s] → %d variable(s)", cp.SourceName, action, len(cp.VarNames))
			} else {
				label = fmt.Sprintf("context %q → %q [%s] (%d variable(s))", cp.SourceName, cp.DestName, action, len(cp.VarNames))
			}
			ren.Item(action, label, "")
			for _, v := range cp.VarNames {
				fmt.Fprintf(out, "      %s\n", v)
			}
			if len(cp.BlockingRestrictions) > 0 {
				fmt.Fprintf(out, "    WARN: %d project/expression restriction(s) — use --remove-restrictions or --host-project to a permitted project\n",
					len(cp.BlockingRestrictions))
			}
		}
	}

	// ── Project secrets section ───────────────────────────────────────────────
	if len(plan.Projects) > 0 {
		ren.Section("Project secrets", "one pipeline per source project")
		for _, pp := range plan.Projects {
			if pp.Skipped {
				ren.Item("skipped", fmt.Sprintf("SKIP project %q", pp.SourceSlug), pp.SkipReason)
			} else {
				sshNote := ""
				if len(pp.SSHKeys) > 0 {
					sshNote = fmt.Sprintf(", %d ssh key(s)", len(pp.SSHKeys))
				}
				label := fmt.Sprintf("project %q → %q (%d variable(s)%s) [pipeline under %s]",
					pp.SourceSlug, pp.DestSlug, len(pp.VarNames), sshNote, pp.SourceSlug)
				ren.Item("created", label, "")
				for _, v := range pp.VarNames {
					fmt.Fprintf(out, "      var: %s\n", v)
				}
				for _, k := range pp.SSHKeys {
					hostLabel := k.Hostname
					if hostLabel == "" {
						hostLabel = "(global)"
					}
					fmt.Fprintf(out, "      ssh: fp=%s host=%s\n", k.Fingerprint, hostLabel)
				}
			}
		}
	}

	// ── Totals ────────────────────────────────────────────────────────────────
	activeProjCount := 0
	for _, pp := range plan.Projects {
		if !pp.Skipped {
			activeProjCount++
		}
	}
	fmt.Fprintln(out, "")
	if activeProjCount > 0 {
		fmt.Fprintf(out, "  Total: %d context(s), %d context variable(s); %d project(s), %d project variable(s), %d ssh key(s)\n",
			len(plan.Contexts), plan.TotalVars(), activeProjCount, plan.TotalProjectVars(), plan.TotalSSHKeys())
	} else {
		fmt.Fprintf(out, "  Total: %d context(s), %d variable(s)\n", len(plan.Contexts), plan.TotalVars())
	}

	// ── Security note on stderr ───────────────────────────────────────────────
	fmt.Fprintln(errOut, "")
	if errRen.ColorEnabled() {
		// print a highlighted security note
		fmt.Fprintf(errOut, "  SECURITY NOTE: the dest API token must already be stored in the source org context\n")
	} else {
		fmt.Fprintf(errOut, "SECURITY NOTE: the dest API token must already be stored in the source org context\n")
	}
	fmt.Fprintf(errOut, "  %q (env var: %s).\n", plan.DestTokenContext, plan.DestTokenEnvVar)
	fmt.Fprintln(errOut, "  Source org admins with access to that context can read the dest token.\n  Use a scoped token and rotate it after transfer.")
}

// pollWorkflow blocks until the pipeline has a terminal workflow, then returns
// it.  It returns an error if ctx is cancelled.
func pollWorkflow(ctx context.Context, poller WorkflowPoller, pipelineID string, interval time.Duration, errOut io.Writer) (project.Workflow, error) {
	for {
		// Check the pipeline state FIRST. An "errored" pipeline (e.g. a
		// config-fetch failure) produces NO workflows, so a workflow-only poll
		// would hang until the context deadline. Fail fast with the real error.
		if pl, err := poller.GetPipeline(ctx, pipelineID); err == nil && pl.State == "errored" {
			msg := "pipeline errored before any workflow ran"
			if len(pl.Errors) > 0 {
				msg = pl.Errors[0].Message
			}
			return project.Workflow{}, fmt.Errorf("pipeline %q errored: %s", pipelineID, msg)
		}

		workflows, err := poller.GetPipelineWorkflows(ctx, pipelineID)
		if err != nil {
			return project.Workflow{}, fmt.Errorf("GetPipelineWorkflows: %w", err)
		}

		for _, wf := range workflows {
			if terminalStatuses[wf.Status] {
				return wf, nil
			}
		}

		fmt.Fprintf(errOut, "  waiting for pipeline %s…\n", pipelineID)

		select {
		case <-ctx.Done():
			return project.Workflow{}, fmt.Errorf("poll timed out waiting for pipeline %q: %w", pipelineID, ctx.Err())
		case <-time.After(interval):
			// continue polling
		}
	}
}
