package cmd

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/capture"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/clog"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/version"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// run executes the capture orchestration for the bound flags.
func (cf *captureFlags) run(cmd *cobra.Command) error {
	// ── Interactive guided mode ────────────────────────────────────────
	// Fire when on a TTY and not --no-input AND not enough flags to run
	// non-interactively (manifest path is the minimum required flag).
	missingManifest := cf.manifestPath == ""
	wantsInteraction := missingManifest && !cf.noInput

	if wantsInteraction && !isInteractiveTTY() {
		// Non-TTY with missing required flags: fail clearly.
		return fmt.Errorf("--manifest is required in non-interactive mode; " +
			"run 'secrets capture' on an interactive terminal for the guided walkthrough")
	}

	if wantsInteraction {
		// Launch interactive walkthrough.
		// For storage: only pre-fill if the user explicitly changed the flag
		// (otherwise the default "artifact" would suppress the storage prompt).
		walkthroughStorage := ""
		if cmd.Flags().Changed("storage") {
			walkthroughStorage = cf.encOpts.storage
		}
		// For encrypt: reset to zero so the walkthrough always asks.
		// The flag defaults to true but we never want to suppress step 4.
		walkthroughEncOpts := cf.encOpts
		walkthroughEncOpts.storage = walkthroughStorage
		walkthroughEncOpts.encrypt = false // walkthrough asks; don't pre-fill

		// For cf.branch, cf.output, and enable-trigger: only pre-fill if user
		// explicitly set them (otherwise the default values would suppress
		// the walkthrough prompts for those options).
		walkthroughBranch := ""
		if cmd.Flags().Changed("cf.branch") {
			walkthroughBranch = cf.branch
		}
		walkthroughEnableTrigger := false
		if cmd.Flags().Changed("enable-trigger") {
			walkthroughEnableTrigger = cf.enableTrigger
		}
		walkthroughOutput := ""
		if cmd.Flags().Changed("cf.output") {
			walkthroughOutput = cf.output
		}

		initial := CaptureWalkthroughResult{
			ManifestPath:          cf.manifestPath,
			Output:                walkthroughOutput,
			ProjectSlugs:          cf.projectSlugs,
			ContextNames:          cf.contextNames,
			HostProjectSlug:       cf.hostProjectSlug,
			Branch:                walkthroughBranch,
			EnableTrigger:         walkthroughEnableTrigger,
			ArtifactRetentionDays: cf.artifactRetentionDays,
			EncOpts:               walkthroughEncOpts,
		}
		result, wErr := runCaptureWalkthrough(cmd, initial)
		if wErr != nil {
			return wErr
		}
		// Apply walkthrough results back to local vars.
		cf.manifestPath = result.ManifestPath
		cf.output = result.Output
		cf.projectSlugs = result.ProjectSlugs
		cf.contextNames = result.ContextNames
		cf.hostProjectSlug = result.HostProjectSlug
		if result.Branch != "" {
			cf.branch = result.Branch
		}
		cf.enableTrigger = result.EnableTrigger
		cf.artifactRetentionDays = result.ArtifactRetentionDays
		// Merge cf.encOpts: walkthrough may have set storage, encrypt, etc.
		if result.EncOpts.storage != "" {
			cf.encOpts.storage = result.EncOpts.storage
		}
		// Always take the walkthrough's encrypt decision (it asked the user).
		cf.encOpts.encrypt = result.EncOpts.encrypt
		if result.EncOpts.encrypt {
			cf.encOpts.generateKey = result.EncOpts.generateKey
			cf.encOpts.sshPublicKey = result.EncOpts.sshPublicKey
			cf.encOpts.sshPrivateKey = result.EncOpts.sshPrivateKey
		}
		if result.EncOpts.s3Bucket != "" {
			cf.encOpts.s3Bucket = result.EncOpts.s3Bucket
		}
		if result.EncOpts.s3Prefix != "" {
			cf.encOpts.s3Prefix = result.EncOpts.s3Prefix
		}
	}

	if cf.manifestPath == "" {
		return errors.New("--manifest is required")
	}

	// ── Resolve encrypt/no-encrypt after walkthrough ─────────────────
	// In non-interactive mode (wantsInteraction==false), apply the
	// --no-encrypt override and handle the "no explicit choice" case.
	if !wantsInteraction {
		encryptChanged := cmd.Flags().Changed("encrypt")
		noEncryptChanged := cmd.Flags().Changed("no-encrypt")
		generateKeyChanged := cmd.Flags().Changed("generate-key")
		sshPubKeyChanged := cmd.Flags().Changed("ssh-public-key")

		// In --no-input mode every unattended run must carry an explicit
		// encryption decision so there are no silent plaintext captures.
		// Any of these flags count as an explicit choice:
		//   --encrypt, --no-encrypt, --generate-key, --ssh-public-key
		hasExplicitChoice := encryptChanged || noEncryptChanged ||
			generateKeyChanged || sshPubKeyChanged
		if cf.noInput && !hasExplicitChoice {
			return fmt.Errorf(
				"--no-input requires an explicit encryption choice: " +
					"use --generate-key (encrypt with auto-generated key), " +
					"--ssh-public-key <path> (encrypt with existing key), " +
					"or --no-encrypt (opt out — NOT recommended for production secrets)")
		}

		if noEncryptChanged {
			// --no-encrypt was explicitly passed; override the default.
			cf.encOpts.encrypt = !cf.noEncrypt
		}
		// else: cf.encOpts.encrypt retains its default (true) or the value
		// set by --encrypt if the user passed that flag explicitly.
	}

	// When encrypt is enabled and no key material supplied, auto-generate.
	// This matches the walkthrough's "generate-key" default and allows
	// zero-argument non-interactive use: `secrets capture --manifest m.json`.
	if cf.encOpts.encrypt && !cf.encOpts.generateKey && cf.encOpts.sshPublicKey == "" {
		cf.encOpts.generateKey = true
		clog.Infof("capture: no key supplied with --encrypt; auto-enabling --generate-key")
	}

	cfg := configFromContext(cmd.Context())
	token := cfg.SourceTokenOrDefault()
	if token == "" {
		return noSourceTokenError()
	}

	// ── Resolve encryption options ────────────────────────────────────
	if cf.encOpts.encrypt {
		if err := resolveEncryptOpts(cmd, &cf.encOpts); err != nil {
			return err
		}
	}

	// ── Validate storage flags ────────────────────────────────────────
	if err := validateStorageFlags(&cf.encOpts); err != nil {
		return err
	}

	// ── Fail-closed guard: unattended capture-all (#164) ──────────────
	// Once flag/storage/token validation has passed but BEFORE any
	// CircleCI client is created or any extraction pipeline is triggered,
	// refuse to proceed when the run is non-interactive, neither --context
	// nor --project scoped it, and no explicit unattended opt-in (--yes or
	// --no-input) was given. Without this guard a piped/recorded/CI session
	// could accidentally sweep EVERY context/project with values and fire
	// real extraction pipelines org-wide.
	if err := GuardUnattendedCaptureAll(
		wantsInteraction,
		cmd.Flags().Changed("context"),
		cmd.Flags().Changed("project"),
		cf.assumeYes,
		cf.noInput,
	); err != nil {
		return err
	}

	m, err := manifest.Load(cf.manifestPath)
	if err != nil {
		return err
	}

	bndl, err := loadOrNewBundle(cf.output)
	if err != nil {
		return err
	}

	projClient, err := project.NewClient(cfg, token)
	if err != nil {
		return fmt.Errorf("creating project client: %w", err)
	}

	ctxClient, err := newContextClientForCapture(cfg, token)
	if err != nil {
		return fmt.Errorf("creating context client: %w", err)
	}

	capClient := &capture.CombinedClient{
		FlagReaderWriter:          projClient,
		PipelineDefLister:         projClient,
		ProjectGetter:             projClient,
		ContextRestrictionManager: ctxClient,
		Deps:                      projClient,
	}

	// ── Scoping (Fix 2: context/project separation) ───────────────────
	// Determine whether the caller explicitly chose contexts and/or projects.
	// explicitContexts: --context was provided → capture exactly those.
	// explicitProjects: --project was provided → capture exactly those.
	//
	// When neither flag is given both sets default to "with values":
	//   contexts → all that have ≥1 env var
	//   projects → all that have ≥1 env var (via selectProjects default)
	//
	// When --context is given but NOT --project, we ONLY run context
	// extraction under the host project; the per-project loop is skipped
	// entirely.  This prevents accidental full-org sweeps.
	explicitContexts := len(cf.contextNames) > 0
	explicitProjects := len(cf.projectSlugs) > 0

	// Resolve the set of projects to process for project-env extraction.
	// - Explicit: use the given slugs.
	// - Default: projects with ≥1 env var (selectProjects no-slug path).
	// - If --context given without --project: skip project loop entirely.
	var projects []manifest.Project
	runProjectLoop := true
	if explicitContexts && !explicitProjects {
		// Context-only request: do not run per-project env-var extraction.
		runProjectLoop = false
	} else {
		projects = capture.SelectProjects(m, cf.projectSlugs)
	}

	if len(projects) == 0 && cf.hostProjectSlug == "" && !explicitContexts {
		return fmt.Errorf("no projects matched the given selectors (manifest has %d projects)", len(m.Projects))
	}

	// Pre-resolve the set of context names the caller wants to include.
	// Empty selectedCtxNames means "include all contexts with values".
	selectedCtxNames := make(map[string]bool, len(cf.contextNames))
	for _, n := range cf.contextNames {
		selectedCtxNames[n] = true
	}

	// When no --context flag was given, default to contexts-with-values.
	// Build a filter from the manifest itself.
	if !explicitContexts {
		for i := range m.Contexts {
			if len(m.Contexts[i].EnvVars) > 0 {
				selectedCtxNames[m.Contexts[i].Name] = true
			}
		}
	}

	// ── Org-level allow_api_trigger_with_config ───────────────────────
	// The pipeline/run endpoint also requires the ORG-level flag to be on.
	// We check/enable it ONCE before iterating projects (not per-project).
	//
	// Behaviour:
	//   --enable-trigger set → enable if off, defer restore (existing path).
	//   --enable-trigger NOT set:
	//     flag already ON → proceed, no prompt.
	//     flag OFF + interactive TTY → offer to enable (with auto-restore on no).
	//     flag OFF + non-interactive → fail fast with actionable error.
	//     flag read error → warn and continue (best-effort).
	if vcsType, orgName, ok := capture.ParseOrgSlug(m.Source.Org.Slug); ok {
		orgClient, oerr := newOrgClientForCapture(cfg, token)
		if oerr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(),
				"WARNING: could not create org client to check org-level flag: %v\n", oerr)
		} else {
			restoreOrgFlag, ferr := checkAndMaybeEnableOrgTriggerFlag(
				cmd, cf, orgClient, vcsType, orgName,
			)
			if ferr != nil {
				return ferr
			}
			defer restoreOrgFlag()
		}
	}

	// ── Artifact-retention safety control ─────────────────────────────
	// When --artifact-retention-days is set, lower the artifact-retention
	// BEFORE triggering the extraction pipeline so that any secrets that
	// land in the build artifact expire quickly.  We deliberately do NOT
	// auto-restore: keeping artifact retention low is the safe default
	// when secrets may be present in artifacts.
	if cf.artifactRetentionDays > 0 && m.Source.Org.ID != "" {
		orgRetentionClient, oerr := newOrgClientForCapture(cfg, token)
		if oerr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(),
				"WARNING: could not create org client for artifact-retention control: %v\n", oerr)
		} else {
			capture.ApplyArtifactRetentionControl(cmd.Context(), cmd.ErrOrStderr(), orgRetentionClient, m.Source.Org.ID, cf.artifactRetentionDays)
		}
	}

	var captureErr error

	// ── Bug 5: Build restriction decider ─────────────────────────────
	// In interactive mode (wantsInteraction was true earlier), ask the user
	// about restricted contexts instead of silently skipping them.
	// In non-interactive / --no-input mode, use nil (falls back to
	// skipRestricted / cf.removeRestrictions flag logic).
	var resDecider capture.RestrictionDecider
	if wantsInteraction && isInteractiveTTY() {
		prompter := NewPrompter(os.Stdin, cmd.ErrOrStderr())
		resDecider = func(ctxName string, n int) (bool, error) {
			return prompter.askBool(
				fmt.Sprintf("Context %q has %d restriction(s) — temporarily remove them to extract (restored afterward)?", ctxName, n),
				false,
			)
		}
	}

	// ── Auto-pick host project when contexts need extraction ─────────────
	// If contexts are selected (by flag or by default-with-values) and no
	// --host-project was given, auto-pick:
	//   1. First project that has ≥1 pipeline definition (live check later).
	//   2. Fall back: first project in the manifest.
	// We record the auto-picked slug here and let captureProject do the live
	// definition check; if it returns errSkipProject we stop.
	hasContextsToCapture := len(selectedCtxNames) > 0
	if hasContextsToCapture && cf.hostProjectSlug == "" {
		if len(m.Projects) == 0 {
			return fmt.Errorf("cannot extract contexts: no projects found in manifest (a host project is required)")
		}
		cf.hostProjectSlug = m.Projects[0].Slug
		clog.Infof("capture: auto-picked host project %s for context extraction", cf.hostProjectSlug)
		fmt.Fprintf(cmd.ErrOrStderr(), "Auto-picking host project %s for context extraction (use --host-project to override).\n", cf.hostProjectSlug)
	}

	// ── Capture scope summary ─────────────────────────────────────────────
	// Print a one-line operator summary before starting any pipelines.
	{
		nCtx := len(selectedCtxNames)
		nProj := len(projects)
		fmt.Fprintf(cmd.ErrOrStderr(),
			"Capture scope: %d context(s) with values, %d project(s) with values, host project: %s\n",
			nCtx, nProj, func() string {
				if cf.hostProjectSlug != "" {
					return cf.hostProjectSlug
				}
				return "(none)"
			}(),
		)
	}

	// ── Context extraction via host project ───────────────────────────────
	// Context env vars are extracted ONCE under the host project — NOT in
	// each per-project run.  This is the key safety invariant: a context's
	// secrets are never dumped multiple times or under unintended projects.
	if hasContextsToCapture && cf.hostProjectSlug != "" {
		hostProjManifest := capture.FindProjectBySlug(m, cf.hostProjectSlug)
		if hostProjManifest == nil {
			// Host project not in manifest; synthesise a minimal entry so
			// CaptureProject can still look it up via the API.
			hostProjManifest = &manifest.Project{Slug: cf.hostProjectSlug}
		}
		clog.Infof("capture: running CONTEXT extraction under host project %s", cf.hostProjectSlug)
		fmt.Fprintf(cmd.ErrOrStderr(), "Running CONTEXT extraction under host project %s…\n", cf.hostProjectSlug)
		// ProjectVarsOnly=false: capture contexts (and host project's own vars).
		// CaptureSSHKeys=false: SSH key extraction is only done in project-vars
		// mode (ProjectVarsOnly=true) so it runs under the correct project.
		if err := capture.CaptureProject(cmd.Context(), capture.CaptureProjectOptions{
			Client:             capClient,
			Manifest:           m,
			Bundle:             bndl,
			Project:            hostProjManifest,
			SelectedCtxNames:   selectedCtxNames,
			Branch:             cf.branch,
			Output:             cf.output,
			EnableTrigger:      cf.enableTrigger,
			SkipRestricted:     cf.skipRestrictedCtxs,
			RemoveRestrictions: cf.removeRestrictions,
			PollTimeout:        cf.pollTimeout,
			Encrypt:            cf.encOpts.toCaptureEncrypt(),
			RestrictDecider:    resDecider,
			ProjectVarsOnly:    false, // include context extraction
			CaptureSSHKeys:     false, // not in host-project context run
			Stdout:             cmd.OutOrStdout(),
			Stderr:             cmd.ErrOrStderr(),
		}); err != nil {
			if errors.Is(err, capture.ErrSkipProject) {
				// Not a hard error; already printed a SKIP notice.
			} else {
				fmt.Fprintf(cmd.ErrOrStderr(), "ERROR capturing contexts under host project %s: %v\n", cf.hostProjectSlug, err)
				if captureErr == nil {
					captureErr = err
				}
			}
		}
	}

	// ── Project env-var extraction (Fix 1) ───────────────────────────────
	// Each selected project's own env vars are captured under that project's
	// pipeline with projectVarsOnly=true.  This means the context loop is
	// SKIPPED entirely — contexts were already extracted under the host
	// project above.  An empty context filter is NOT enough here: even an
	// empty selectedCtxNames would still pass through the context loop and
	// could trigger unwanted pipeline attaches.
	if runProjectLoop {
		for i := range projects {
			p := &projects[i]
			if err := capture.CaptureProject(cmd.Context(), capture.CaptureProjectOptions{
				Client:             capClient,
				Manifest:           m,
				Bundle:             bndl,
				Project:            p,
				SelectedCtxNames:   nil, // unused when ProjectVarsOnly=true
				Branch:             cf.branch,
				Output:             cf.output,
				EnableTrigger:      cf.enableTrigger,
				SkipRestricted:     cf.skipRestrictedCtxs,
				RemoveRestrictions: cf.removeRestrictions,
				PollTimeout:        cf.pollTimeout,
				Encrypt:            cf.encOpts.toCaptureEncrypt(),
				RestrictDecider:    resDecider,
				ProjectVarsOnly:    true,              // skip context loop entirely
				CaptureSSHKeys:     cf.sshKeysCapture, // capture SSH private keys when flag is set
				Stdout:             cmd.OutOrStdout(),
				Stderr:             cmd.ErrOrStderr(),
			}); err != nil {
				if errors.Is(err, capture.ErrSkipProject) {
					// Not a hard error; SKIP notice already printed in CaptureProject.
				} else {
					// Continue processing other projects; record the first error.
					fmt.Fprintf(cmd.ErrOrStderr(), "ERROR capturing project %s: %v\n", p.Slug, err)
					if captureErr == nil {
						captureErr = err
					}
				}
			}
		}
	}

	bndl.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	bndl.ToolVersion = version.UserAgent()
	if err := bndl.Save(cf.output); err != nil {
		return fmt.Errorf("writing secret bundle: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Secret bundle written to %s\n", cf.output)

	if cf.encOpts.encrypt {
		fmt.Fprintln(cmd.ErrOrStderr(), `
NOTE: Encryption was enabled. The CircleCI artifact was age-encrypted.
  Plaintext secrets never persisted in CircleCI artifact storage.
  The local bundle at `+cf.output+` contains plaintext. Protect it.`)
	} else {
		fmt.Fprintln(cmd.ErrOrStderr(), `
WARNING: --no-encrypt was set. The secret bundle contains PLAINTEXT secrets.
  - Protect the file; do not commit it to version control.
  - Build artifacts from the extraction run are retained for at least 1 day.
    There is NO delete-artifact API. Rotate captured secrets and treat the
    artifact as sensitive until it expires.`)
	}

	return captureErr
}

// checkAndMaybeEnableOrgTriggerFlag performs the pre-flight check on the
// org-level allow_api_trigger_with_config flag before any pipeline is triggered.
//
// Decision matrix:
//
//	enableTrigger=true                  → enable if off, return restore func (MaybeEnableOrgTriggerFlag).
//	enableTrigger=false, flag ON        → proceed, no-op restore.
//	enableTrigger=false, flag OFF + TTY → prompt; yes → enable+restore; no → error.
//	enableTrigger=false, flag OFF + no-TTY → fail-fast actionable error.
//	enableTrigger=false, read error     → warn + return no-op (best-effort).
//
// The returned restore func must be deferred by the caller.
func checkAndMaybeEnableOrgTriggerFlag(
	cmd *cobra.Command,
	cf *captureFlags,
	orgClient capture.OrgFlagManager,
	vcsType, orgName string,
) (restore func(), err error) {
	noop := func() {}

	if cf.enableTrigger {
		// Existing path: user explicitly asked us to enable+restore.
		restoreFn := capture.MaybeEnableOrgTriggerFlag(cmd.Context(), cmd.ErrOrStderr(), orgClient, vcsType, orgName)
		return restoreFn, nil
	}

	// Read the org flag to determine if it's already on.
	flags, rerr := orgClient.GetFeatureFlags(cmd.Context(), vcsType, orgName)
	if rerr != nil {
		// Warn but do not hard-block — MaybeEnableOrgTriggerFlag already tolerates
		// read failures, so this matches the existing best-effort contract.
		fmt.Fprintf(cmd.ErrOrStderr(),
			"WARNING: could not read org-level feature flags for %s/%s: %v — "+
				"capture may fail if allow_api_trigger_with_config is OFF; "+
				"use --enable-trigger to let the CLI enable it automatically.\n",
			vcsType, orgName, rerr)
		return noop, nil
	}

	if capture.OrgTriggerAlreadyEnabled(flags) {
		// Flag is already ON — nothing to do.
		return noop, nil
	}

	// Flag is OFF. Decide based on TTY.
	if isInteractiveTTY() {
		prompter := NewPrompter(os.Stdin, cmd.ErrOrStderr())
		fmt.Fprintf(cmd.ErrOrStderr(),
			"\nNOTICE: 'secrets capture' triggers an in-pipeline job that requires "+
				"'Allow triggering pipelines with unversioned config' "+
				"(allow_api_trigger_with_config) to be ON for %s/%s.\n"+
				"It is currently OFF.\n",
			vcsType, orgName)
		enable, perr := prompter.askBool(
			"Enable it now for the duration of capture? It will be restored to OFF afterward",
			false,
		)
		if perr != nil {
			return noop, fmt.Errorf("reading prompt: %w", perr)
		}
		if !enable {
			return noop, fmt.Errorf(
				"capture cannot proceed: allow_api_trigger_with_config is OFF for %s/%s; "+
					"re-run with --enable-trigger to let the CLI enable it automatically (restored after capture), "+
					"or enable 'Allow triggering pipelines with unversioned config' in Org Settings -> Advanced",
				vcsType, orgName)
		}
		// User said yes — enable and return a restore func.
		restoreFn := capture.MaybeEnableOrgTriggerFlag(cmd.Context(), cmd.ErrOrStderr(), orgClient, vcsType, orgName)
		return restoreFn, nil
	}

	// Non-interactive: fail fast with an actionable error.
	return noop, fmt.Errorf(
		"allow_api_trigger_with_config is OFF for %s/%s; capture would fail mid-run — "+
			"re-run with --enable-trigger to let the CLI enable it for the duration of capture (auto-restored afterward), "+
			"or enable 'Allow triggering pipelines with unversioned config' in Org Settings -> Advanced",
		vcsType, orgName)
}

// bind registers the capture flags on f and stores their values in cf.
func (cf *captureFlags) bind(f *pflag.FlagSet) {
	f.StringVar(&cf.manifestPath, "manifest", "", "Path to the export manifest (prompted interactively when omitted on a TTY)")
	f.StringVarP(&cf.output, "output", "o", "secrets.json", "Path to the secret bundle to write/append")
	f.StringArrayVar(&cf.projectSlugs, "project", nil, "Project slug(s) to capture (default: all in manifest)")
	f.StringArrayVar(&cf.contextNames, "context", nil, "Context name(s) to capture (default: all in manifest)")
	f.StringVar(&cf.hostProjectSlug, "host-project", "",
		"Project slug to use when running the CONTEXT extraction pipeline. "+
			"Any project works — build history is irrelevant; only the extraction matters. "+
			"Prompted interactively when contexts are selected and this flag is absent.")
	f.StringVar(&cf.branch, "branch", "main", "Branch to check out for the extraction run")
	f.BoolVar(&cf.enableTrigger, "enable-trigger", false,
		"Unconditionally enable allow_api_trigger_with_config for the org before capture and restore it afterward. "+
			"When omitted, capture auto-detects the flag: if it is OFF on an interactive TTY you are offered a "+
			"choice; on a non-interactive terminal capture fails fast with an actionable error.")
	f.BoolVar(&cf.skipRestrictedCtxs, "skip-restricted-contexts", true,
		"Skip contexts that have project/expression/group restrictions (attach warning instead of attempting)")
	f.BoolVar(&cf.removeRestrictions, "remove-restrictions", false,
		"Temporarily remove real context restrictions before extraction and restore them afterwards (requires explicit opt-in)")
	f.BoolVar(&cf.noInput, "no-input", false,
		"Disable all interactive prompts; error if a required value is missing (implied when stdin is not a TTY)")
	f.BoolVarP(&cf.assumeYes, "yes", "y", false,
		"Acknowledge an unattended capture-all when neither --context nor --project is given. "+
			"Without this (or --no-input) a non-interactive run that would sweep EVERY context/project "+
			"with values fails closed instead of triggering real extraction pipelines.")
	f.DurationVar(&cf.pollTimeout, "poll-timeout", 10*time.Minute,
		"Maximum time to wait for each pipeline to complete (0 = no timeout)")
	f.IntVar(&cf.artifactRetentionDays, "artifact-retention-days", 0,
		"Set the org's artifact-retention to this many days BEFORE triggering the extraction pipeline. "+
			"Recommended value: 1 (the minimum). Default 0 = leave unchanged. "+
			"The prior value is logged so you can restore it manually. "+
			"This control is NOT auto-restored after capture — keeping retention low is the safe default "+
			"when secrets may land in artifacts.")

	// ── Encryption flags ──────────────────────────────────────────────────────
	// Encryption is ON by default. --no-encrypt opts out; --encrypt is kept for
	// backward-compatibility (explicitly passing --encrypt is a no-op when
	// encryption is already the default).
	f.BoolVar(&cf.encOpts.encrypt, "encrypt", true,
		"Encrypt the in-pipeline artifact with age so plaintext secrets never persist in CircleCI "+
			"(default: true). Supply --ssh-public-key or --generate-key; if neither is given a fresh "+
			"keypair is auto-generated. Use --no-encrypt to opt out.")
	f.BoolVar(&cf.noEncrypt, "no-encrypt", false,
		"Disable artifact encryption and produce a PLAINTEXT secrets artifact. "+
			"NOT recommended for production secrets — build artifacts are retained for at least 1 day "+
			"and there is no delete-artifact API.")
	f.StringVar(&cf.encOpts.sshPublicKey, "ssh-public-key", "",
		"Path to an SSH public key (.pub) or age recipients file used as the encryption recipient. "+
			"The public key is safe to embed in the pipeline config.")
	f.StringVar(&cf.encOpts.sshPrivateKey, "ssh-private-key", "",
		"Path to an SSH private key or age identity file used to decrypt the artifact locally. "+
			"Defaults to ~/.ssh/id_ed25519 if present and --ssh-public-key points to the matching .pub.")
	f.BoolVar(&cf.encOpts.generateKey, "generate-key", false,
		"Generate a fresh age X25519 keypair for this run. Writes the identity to "+
			"./migration-identity.age and the recipient to ./migration-recipient.txt. "+
			"Use --generate-key instead of --ssh-public-key when you do not have an existing key. "+
			"Auto-enabled when --encrypt is in effect and no key is supplied.")

	// ── Storage flags ─────────────────────────────────────────────────────────
	f.StringVar(&cf.encOpts.storage, "storage", "artifact",
		`Where to store the (optionally encrypted) bundle after extraction.
artifact (default) — store as a CircleCI job artifact.
s3                 — upload to S3 via the aws CLI (requires AWS creds in job).
both               — store in both artifact and S3.`)
	f.StringVar(&cf.encOpts.s3Bucket, "s3-bucket", "",
		"S3 bucket name for --storage s3|both (required when --storage s3 or both)")
	f.StringVar(&cf.encOpts.s3Prefix, "s3-prefix", "",
		"S3 key prefix for --storage s3|both (optional; e.g. 'migration/')")

	// ── SSH-key extraction flag ───────────────────────────────────────────────
	f.BoolVar(&cf.sshKeysCapture, "ssh-keys", true,
		"Extract additional SSH private keys for projects that have cataloged SSH keys in the manifest. "+
			"Runs a separate in-pipeline job using add_ssh_keys with the explicit cataloged fingerprints — "+
			"the checkout/deploy key is never materialised. "+
			"Use --no-ssh-keys to skip SSH key extraction (e.g. when running env-var capture only).")
}
