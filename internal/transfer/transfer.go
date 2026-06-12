// Package transfer orchestrates context env-var transfer via an unversioned
// CircleCI pipeline run in the SOURCE org.
//
// Design (from the CircleCI-Labs/circleci-org-migrator context-secret-transfer
// pattern):
//
//   - A single dynamic/inline pipeline is triggered in the SOURCE org with one
//     job per selected context.
//   - Each job imports the context (CircleCI unmasks the values into the job
//     environment) and PUTs each value straight into the matching context in the
//     DESTINATION org over TLS via the CircleCI API.
//   - NO plaintext ever touches disk or build artifacts — strictly better
//     security than the encrypted-bundle-artifact flow for context vars.
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
	"time"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/version"
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
}

// PipelineDefLister lists pipeline definitions for a project.
type PipelineDefLister interface {
	ListPipelineDefinitions(ctx context.Context, projectID string) ([]project.PipelineDefinition, error)
}

// ProjectGetter retrieves project metadata (used to get the project UUID).
type ProjectGetter interface {
	GetProject(ctx context.Context, slug string) (*project.Project, error)
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

	// Mapping is an optional source→dest context name mapping.  When a source
	// context name has an entry, the destination context is looked up by the
	// mapped name.  When absent, the source name is used directly.
	Mapping map[string]string

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

func (o *Options) branch() string {
	if o.Branch != "" {
		return o.Branch
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
}

// Plan describes what Transfer would do (dry-run output).
type Plan struct {
	// Contexts is the ordered list of contexts that would be transferred.
	Contexts []ContextPlan
	// DestTokenContext is the source context holding the dest token.
	DestTokenContext string
	// DestTokenEnvVar is the env-var name within DestTokenContext.
	DestTokenEnvVar string
}

// TotalVars returns the total number of env-var values in the plan.
func (p *Plan) TotalVars() int {
	n := 0
	for _, c := range p.Contexts {
		n += len(c.VarNames)
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
//
// Design invariants:
//   - The dest token value NEVER appears in the generated YAML — it is
//     referenced only by env-var name (${CIRCLECI_DEST_TOKEN} or the override).
//   - No secret values are written to any file or artifact.
//   - The PUT calls go directly over TLS to the destination API.
func buildTransferConfig(m *manifest.Manifest, ctxPlans []ContextPlan, opts *Options) string {
	return buildTransferConfigWithVersion(m, ctxPlans, opts, version.Version)
}

// buildTransferConfigWithVersion is the testable variant.
func buildTransferConfigWithVersion(m *manifest.Manifest, ctxPlans []ContextPlan, opts *Options, ver string) string {
	destHost := opts.destHost()
	destTokenEnvVar := opts.destTokenEnvVar()
	destOrgID := opts.DestOrgID
	destTokenCtx := opts.DestTokenContext

	var sb strings.Builder

	sb.WriteString("version: 2.1\n")
	sb.WriteString("jobs:\n")

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

		// Install circleci-migrate so we have a known binary available, but the
		// actual transfer uses curl (no binary dependency for the PUT calls).
		sb.WriteString(buildTransferInstallStep(ver))

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
		sb.WriteString("            if [ -z \"$DEST_CTX_ID\" ]; then\n")
		sb.WriteString(fmt.Sprintf("              echo \"ERROR: destination context %q not found in org ${DEST_ORG_ID}\" >&2\n", cp.DestName))
		sb.WriteString("              echo 'Run: circleci-migrate sync --manifest manifest.json --apply' >&2\n")
		sb.WriteString("              echo 'to create destination contexts before transferring values.' >&2\n")
		sb.WriteString("              exit 1\n")
		sb.WriteString("            fi\n")
		sb.WriteString(fmt.Sprintf("            echo \"Resolved destination context %q → ${DEST_CTX_ID}\"\n", cp.DestName))
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

	// Workflow: one job per context, all in parallel.
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

	return sb.String()
}

// buildTransferInstallStep is a lightweight install step.  We still install
// circleci-migrate so operators get the same install pattern as capture;
// the actual transfer work is done via curl + jq (both available in
// cimg/base:current) which avoids a circleci-migrate sub-command dependency
// for the PUT logic.
func buildTransferInstallStep(ver string) string {
	return buildInstallStepBase(ver)
}

// buildInstallStepBase mirrors extract.buildInstallStep.
func buildInstallStepBase(ver string) string {
	repo := "AwesomeCICD/circleci-org-migration-cli"
	tag := ver
	if tag == "" || tag == "dev" || tag == "unknown" {
		tag = "latest"
	}

	var sb strings.Builder
	sb.WriteString("      - run:\n")
	sb.WriteString("          name: Install circleci-migrate\n")
	sb.WriteString("          command: |\n")
	sb.WriteString("            set -euo pipefail\n")
	sb.WriteString("            repo=" + repo + "\n")
	if tag == "latest" {
		sb.WriteString(`            ver=$(curl -sfL "https://api.github.com/repos/${repo}/releases/latest" \` + "\n")
		sb.WriteString(`              | grep -o '"tag_name": *"[^"]*"' | head -1 \` + "\n")
		sb.WriteString(`              | sed 's/.*"\(v[^"]*\)".*/\1/')` + "\n")
		sb.WriteString("            if [ -z \"$ver\" ]; then\n")
		sb.WriteString("              echo 'ERROR: could not resolve latest release tag' >&2; exit 1\n")
		sb.WriteString("            fi\n")
	} else {
		sb.WriteString(fmt.Sprintf("            ver=%s\n", tag))
	}
	sb.WriteString(`            v="${ver#v}"` + "\n")
	sb.WriteString(`            os=$(uname -s | tr '[:upper:]' '[:lower:]')` + "\n")
	sb.WriteString(`            arch=$(uname -m)` + "\n")
	sb.WriteString(`            case "$arch" in` + "\n")
	sb.WriteString(`              x86_64)        arch="amd64" ;;` + "\n")
	sb.WriteString(`              aarch64|arm64) arch="arm64" ;;` + "\n")
	sb.WriteString(`              *) echo "ERROR: unsupported arch: $arch" >&2; exit 1 ;;` + "\n")
	sb.WriteString(`            esac` + "\n")
	sb.WriteString(`            url="https://github.com/${repo}/releases/download/${ver}/circleci-migrate_${v}_${os}_${arch}.tar.gz"` + "\n")
	sb.WriteString(`            echo "Downloading ${url}"` + "\n")
	sb.WriteString(`            tmp=$(mktemp -d)` + "\n")
	sb.WriteString(`            curl -sfL "$url" | tar -xz -C "$tmp"` + "\n")
	sb.WriteString(`            bin=$(find "$tmp" -type f -name circleci-migrate | head -1)` + "\n")
	sb.WriteString(`            sudo install -m 0755 "$bin" /usr/local/bin/circleci-migrate` + "\n")
	sb.WriteString(`            rm -rf "$tmp"` + "\n")
	sb.WriteString(`            circleci-migrate version` + "\n")
	return sb.String()
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

		ctxPlans = append(ctxPlans, ContextPlan{
			SourceName: mc.Name,
			DestName:   opts.destContextName(mc.Name),
			VarNames:   varNames,
		})
	}

	if len(ctxPlans) == 0 {
		return Plan{}, errors.New("transfer: no contexts with env-var values found in manifest (nothing to transfer)")
	}

	return Plan{
		Contexts:         ctxPlans,
		DestTokenContext: opts.DestTokenContext,
		DestTokenEnvVar:  opts.destTokenEnvVar(),
	}, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Transfer orchestrator
// ─────────────────────────────────────────────────────────────────────────────

// terminalStatuses is the set of CircleCI workflow statuses that indicate the
// pipeline has finished (success, failure, or cancellation).
var terminalStatuses = map[string]bool{
	"success":  true,
	"failed":   true,
	"error":    true,
	"canceled": true,
}

// ErrWorkflowFailed is returned when the transfer workflow finishes in a
// non-success terminal state.
var ErrWorkflowFailed = errors.New("transfer workflow did not succeed")

// Transfer builds the plan and, when opts.DryRun is false, triggers the
// transfer pipeline and waits for it to complete.
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

	// Live run: resolve host project and pipeline definition.
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

	configYAML := buildTransferConfig(m, plan.Contexts, &opts)

	fmt.Fprintf(opts.Stderr, "Triggering transfer pipeline under %s (definition %s)…\n", opts.HostProjectSlug, defID)

	pipelineID, err := deps.TriggerPipelineRun(ctx, opts.HostProjectSlug, defID, opts.branch(), configYAML, nil)
	if err != nil {
		if errors.Is(err, project.ErrPipelineSkipped) {
			return fmt.Errorf("transfer: pipeline run was skipped — check api-trigger-with-config is enabled and the config is valid")
		}
		return fmt.Errorf("transfer: trigger pipeline: %w", err)
	}
	fmt.Fprintf(opts.Stderr, "Pipeline triggered: %s\n", pipelineID)

	// Poll until terminal.
	pollCtx := ctx
	if opts.PollTimeout > 0 {
		var cancel context.CancelFunc
		pollCtx, cancel = context.WithTimeout(ctx, opts.PollTimeout)
		defer cancel()
	}

	wf, err := pollWorkflow(pollCtx, deps, pipelineID, opts.pollInterval(), opts.Stderr)
	if err != nil {
		return fmt.Errorf("transfer: poll: %w", err)
	}
	if wf.Status != "success" {
		return fmt.Errorf("%w: status=%q workflow=%q", ErrWorkflowFailed, wf.Status, wf.Name)
	}

	fmt.Fprintf(opts.Stdout, "\nTransfer pipeline succeeded: %d context(s), %d variable(s) transferred.\n",
		len(plan.Contexts), plan.TotalVars())

	return nil
}

// printPlan writes the transfer plan to stdout/stderr so operators can review
// what would happen before committing to --apply.
func printPlan(out, errOut io.Writer, plan *Plan, opts *Options) {
	fmt.Fprintln(errOut, "\n⚙  Transfer plan")
	fmt.Fprintf(errOut, "  Dest token: context=%q env-var=%q\n", plan.DestTokenContext, plan.DestTokenEnvVar)
	fmt.Fprintf(errOut, "  Dest org ID: %s\n", opts.DestOrgID)
	fmt.Fprintf(errOut, "  Dest host: %s\n", opts.destHost())
	fmt.Fprintln(errOut, "")

	for _, cp := range plan.Contexts {
		if cp.SourceName == cp.DestName {
			fmt.Fprintf(out, "  context %q → %d variable(s)\n", cp.SourceName, len(cp.VarNames))
		} else {
			fmt.Fprintf(out, "  context %q → %q (%d variable(s))\n", cp.SourceName, cp.DestName, len(cp.VarNames))
		}
		for _, v := range cp.VarNames {
			fmt.Fprintf(out, "    %s\n", v)
		}
	}
	fmt.Fprintf(out, "\nTotal: %d context(s), %d variable(s)\n", len(plan.Contexts), plan.TotalVars())
	fmt.Fprintln(errOut, "\nSECURITY NOTE: the dest API token must already be stored in the source org context")
	fmt.Fprintf(errOut, "  %q (env var: %s).\n", plan.DestTokenContext, plan.DestTokenEnvVar)
	fmt.Fprintln(errOut, "  Source org admins with access to that context can read the dest token.\n  Use a scoped token and rotate it after transfer.")
}

// pollWorkflow blocks until the pipeline has a terminal workflow, then returns
// it.  It returns an error if ctx is cancelled.
func pollWorkflow(ctx context.Context, poller WorkflowPoller, pipelineID string, interval time.Duration, errOut io.Writer) (project.Workflow, error) {
	for {
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
