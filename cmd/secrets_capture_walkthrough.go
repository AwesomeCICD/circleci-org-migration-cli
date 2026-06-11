// Interactive guided walkthrough for 'secrets capture'. Split out of
// secrets_capture.go to keep that file focused on command wiring; this is
// still cmd-layer code (uses Prompter + cobra streams).
package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/clog"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
	"github.com/spf13/cobra"
)

// CaptureWalkthroughResult holds all the values collected by the interactive
// guided walkthrough for 'secrets capture'.  Fields are exported so that
// external test packages (cmd_test) can construct and inspect the result.
type CaptureWalkthroughResult struct {
	ManifestPath          string
	Output                string
	ProjectSlugs          []string
	ContextNames          []string
	HostProjectSlug       string // project used to run CONTEXT extraction
	Branch                string
	EnableTrigger         bool
	ArtifactRetentionDays int
	EncOpts               captureEncryptOpts
}

// runCaptureWalkthrough launches the interactive guided walkthrough for the
// capture command. It writes prompts to cmd.ErrOrStderr() and reads answers
// from os.Stdin.  The function delegates to RunCaptureWalkthroughWith so that
// tests can inject synthetic I/O via NewPrompter.
func runCaptureWalkthrough(
	cmd *cobra.Command,
	initial CaptureWalkthroughResult,
) (CaptureWalkthroughResult, error) {
	return RunCaptureWalkthroughWith(
		NewPrompter(os.Stdin, cmd.ErrOrStderr()),
		cmd,
		initial,
	)
}

// RunCaptureWalkthroughWith is the injectable interactive walkthrough used by
// both the command (via runCaptureWalkthrough) and external test files.
// p supplies the I/O streams; cmd is used for printing the confirmation summary.
func RunCaptureWalkthroughWith(
	p *Prompter,
	cmd *cobra.Command,
	initial CaptureWalkthroughResult,
) (CaptureWalkthroughResult, error) {
	out := p.out
	res := initial

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "╔══════════════════════════════════════════════════════════╗")
	fmt.Fprintln(out, "║  CircleCI Secret Capture — guided mode (RECOMMENDED)     ║")
	fmt.Fprintln(out, "╚══════════════════════════════════════════════════════════╝")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "This guided flow extracts secret values from CircleCI WITHOUT committing")
	fmt.Fprintln(out, "any config to your repositories. The CLI builds an inline pipeline config,")
	fmt.Fprintln(out, "triggers a run, and downloads the captured values automatically.")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Tip: re-run with all flags set to bypass these prompts (CI-safe).")

	// ── Step 1: Manifest path ─────────────────────────────────────────────────
	printStepHeader(out, 1, 8, "Manifest")
	fmt.Fprintln(out, "  The manifest records variable names for all contexts and projects.")

	var err error
	if res.ManifestPath == "" {
		res.ManifestPath, err = p.askWithDefault("Manifest file path", "manifest.json")
		if err != nil {
			return res, err
		}
		if res.ManifestPath == "" {
			res.ManifestPath = "manifest.json"
		}
	} else {
		fmt.Fprintf(out, "  Manifest: %s  (from --manifest)\n", res.ManifestPath)
	}

	// Load the manifest so we can offer context/project lists.
	clog.Infof("capture walkthrough: loading manifest from %s", res.ManifestPath)
	m, loadErr := manifest.Load(res.ManifestPath)
	if loadErr != nil {
		return res, fmt.Errorf("loading manifest %s: %w", res.ManifestPath, loadErr)
	}

	// ── Step 2: What to extract ───────────────────────────────────────────────
	printStepHeader(out, 2, 8, "What to extract")
	fmt.Fprintln(out, "  Select the contexts and/or projects to capture.")

	// Collect context names from manifest.
	ctxOptions := make([]string, 0, len(m.Contexts))
	for _, mc := range m.Contexts {
		ctxOptions = append(ctxOptions, mc.Name)
	}

	// Collect project slugs from manifest. Also build display labels (Name + slug).
	// projSlugByLabel maps display label → slug so we can convert user selections
	// back to slugs after the multi-select prompt.
	projOptions := make([]string, 0, len(m.Projects))           // display labels
	projSlugByLabel := make(map[string]string, len(m.Projects)) // label → slug

	// Bug 3: default to only projects that have secrets (>=1 env var).
	projWithSecrets := make([]string, 0, len(m.Projects))
	projWithoutSecrets := 0
	for _, mp := range m.Projects {
		label := mp.Slug
		if mp.Name != "" && mp.Name != mp.Slug {
			label = fmt.Sprintf("%s (%s)", mp.Name, mp.Slug)
		}
		projOptions = append(projOptions, label)
		projSlugByLabel[label] = mp.Slug
		if len(mp.EnvVars) > 0 {
			projWithSecrets = append(projWithSecrets, label)
		} else {
			projWithoutSecrets++
		}
	}

	// Helper: convert display labels back to slugs.
	labelsToSlugs := func(labels []string) []string {
		slugs := make([]string, 0, len(labels))
		for _, lbl := range labels {
			if s, ok := projSlugByLabel[lbl]; ok {
				slugs = append(slugs, s)
			} else {
				// Fallback: treat the label itself as a slug (for backward compat).
				slugs = append(slugs, lbl)
			}
		}
		return slugs
	}

	// Only prompt if not already supplied via flags.
	if len(res.ContextNames) == 0 && len(ctxOptions) > 0 {
		chosen, selErr := p.askMultiSelect(
			fmt.Sprintf("Select contexts to capture (%d in manifest, default: all):", len(ctxOptions)),
			ctxOptions,
		)
		if selErr != nil {
			return res, selErr
		}
		res.ContextNames = chosen
	} else if len(ctxOptions) == 0 {
		fmt.Fprintln(out, "  (no contexts found in manifest)")
	} else {
		fmt.Fprintf(out, "  Contexts: %v  (from --context)\n", res.ContextNames)
	}

	if len(res.ProjectSlugs) == 0 && len(projOptions) > 0 {
		// Bug 3: default selection to projects-with-secrets only.
		defaultLabels := projWithSecrets
		if len(defaultLabels) == 0 {
			defaultLabels = projOptions // no projects have secrets → show all
		}
		promptMsg := fmt.Sprintf("Select projects to capture project env vars for (%d in manifest, default: %d with secrets):", len(projOptions), len(projWithSecrets))
		if projWithoutSecrets > 0 {
			fmt.Fprintf(out, "  NOTE: %d project(s) have no env vars in the manifest and are hidden from defaults.\n", projWithoutSecrets)
		}
		chosen, selErr := p.askMultiSelectWithDefault(
			promptMsg,
			projOptions,
			defaultLabels,
		)
		if selErr != nil {
			return res, selErr
		}
		res.ProjectSlugs = labelsToSlugs(chosen)
	} else if len(projOptions) == 0 {
		fmt.Fprintln(out, "  (no projects found in manifest)")
	} else {
		fmt.Fprintf(out, "  Projects: %v  (from --project)\n", res.ProjectSlugs)
	}

	// ── Step 3: Host project for context extraction ───────────────────────────
	printStepHeader(out, 3, 8, "Host project for CONTEXT extraction")
	fmt.Fprintln(out, "  Context env vars are extracted by attaching the context to a pipeline run.")
	fmt.Fprintln(out, "  You must choose which project's pipeline to run this under.")
	fmt.Fprintln(out, "  (Any project works — build history doesn't matter, only the extraction does.)")

	hasContexts := len(res.ContextNames) > 0
	if len(res.ContextNames) == 0 && len(ctxOptions) == 0 {
		hasContexts = false
	}
	// If contextNames is empty slice (user selected none), skip host project.
	if hasContexts && len(res.ContextNames) == 0 {
		hasContexts = false
	}

	if hasContexts {
		if res.HostProjectSlug == "" {
			// Build project display labels for host selection.
			// Prefer to auto-pick the first project if only one available.
			if len(projOptions) == 1 {
				firstSlug := projSlugByLabel[projOptions[0]]
				if firstSlug == "" {
					firstSlug = projOptions[0]
				}
				fmt.Fprintf(out, "  Only one project in manifest — auto-selecting %s as the host project.\n", projOptions[0])
				res.HostProjectSlug = firstSlug
			} else if len(projOptions) == 0 {
				return res, fmt.Errorf("cannot extract contexts: no projects found in manifest (a host project is required to run the extraction pipeline under)")
			} else {
				// Let user choose, or auto-pick first.
				firstSlug := projSlugByLabel[projOptions[0]]
				if firstSlug == "" {
					firstSlug = projOptions[0]
				}
				autoPickOption := "(auto-pick first: " + projOptions[0] + ")"
				hostOptions := append([]string{autoPickOption}, projOptions...)
				chosen, choiceErr := p.askChoice(
					"Choose host project to run the CONTEXT extraction pipeline under:",
					hostOptions,
				)
				if choiceErr != nil {
					return res, choiceErr
				}
				if chosen == autoPickOption {
					res.HostProjectSlug = firstSlug
					fmt.Fprintf(out, "  Host project: %s\n", res.HostProjectSlug)
				} else {
					// Convert label back to slug.
					if s, ok := projSlugByLabel[chosen]; ok {
						res.HostProjectSlug = s
					} else {
						res.HostProjectSlug = chosen
					}
				}
			}
		} else {
			fmt.Fprintf(out, "  Host project: %s  (from --host-project)\n", res.HostProjectSlug)
		}
	} else {
		fmt.Fprintln(out, "  No contexts selected — skipping host project selection.")
	}

	// ── Step 4: Encryption ────────────────────────────────────────────────────
	printStepHeader(out, 4, 8, "Encryption (RECOMMENDED)")
	fmt.Fprintln(out, "  When enabled, the in-pipeline artifact is age-encrypted.")
	fmt.Fprintln(out, "  Plaintext secrets NEVER persist in CircleCI artifact storage.")

	if !res.EncOpts.encrypt && res.EncOpts.sshPublicKey == "" && !res.EncOpts.generateKey {
		doEncrypt, encErr := p.askBool("Encrypt the captured secrets?", true)
		if encErr != nil {
			return res, encErr
		}
		res.EncOpts.encrypt = doEncrypt

		if doEncrypt {
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, "  Encryption key options:")
			fmt.Fprintln(out, "    1) Generate a fresh keypair automatically (recommended, no key required)")
			fmt.Fprintln(out, "    2) Use an existing SSH public key file")
			fmt.Fprintln(out, "")
			keyChoice, keyErr := p.askChoice(
				"How to provide the encryption key?",
				[]string{"generate a fresh keypair (--generate-key)", "use existing SSH public key (--ssh-public-key)"},
			)
			if keyErr != nil {
				return res, keyErr
			}
			if strings.HasPrefix(keyChoice, "generate") {
				res.EncOpts.generateKey = true
				fmt.Fprintln(out, "  A fresh age X25519 keypair will be generated for this run.")
				fmt.Fprintln(out, "  Keep the identity file (migration-identity.age) — you will need it to decrypt.")
			} else {
				pubKeyPath, pkErr := p.askRequired("Path to SSH public key file", "e.g. ~/.ssh/id_ed25519.pub")
				if pkErr != nil {
					return res, pkErr
				}
				res.EncOpts.sshPublicKey = pubKeyPath

				privKeyPath, privErr := p.askWithDefault("Path to SSH private key for local decryption", "~/.ssh/id_ed25519")
				if privErr != nil {
					return res, privErr
				}
				res.EncOpts.sshPrivateKey = privKeyPath
			}
		} else {
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, "  WARNING: captured secrets will be PLAINTEXT in the CircleCI artifact.")
			fmt.Fprintln(out, "  Build artifacts are retained for at least 1 day with no delete API.")
			fmt.Fprintln(out, "  Encryption is strongly recommended for production secrets.")
		}
	} else {
		if res.EncOpts.encrypt {
			fmt.Fprintln(out, "  Encryption: enabled  (from --encrypt)")
		} else {
			fmt.Fprintln(out, "  Encryption: disabled  (from flags)")
		}
	}

	// ── Step 5: Storage ───────────────────────────────────────────────────────
	printStepHeader(out, 5, 8, "Storage")
	fmt.Fprintln(out, "  Where to store the (optionally encrypted) bundle after extraction.")

	if res.EncOpts.storage == "" {
		storageChoice, storErr := p.askChoice(
			"Storage mode for the extracted bundle:",
			[]string{"artifact (default — CircleCI job artifact)", "s3 (S3 upload; requires AWS creds in job)", "both (artifact + S3)"},
		)
		if storErr != nil {
			return res, storErr
		}
		switch {
		case strings.HasPrefix(storageChoice, "s3"):
			res.EncOpts.storage = "s3"
		case strings.HasPrefix(storageChoice, "both"):
			res.EncOpts.storage = "both"
		default:
			res.EncOpts.storage = "artifact"
		}

		if res.EncOpts.storage == "s3" || res.EncOpts.storage == "both" {
			bucket, bErr := p.askRequired("S3 bucket name", "--s3-bucket")
			if bErr != nil {
				return res, bErr
			}
			res.EncOpts.s3Bucket = bucket

			prefix, pErr := p.askWithDefault("S3 key prefix (optional)", "migration/")
			if pErr != nil {
				return res, pErr
			}
			res.EncOpts.s3Prefix = prefix

			fmt.Fprintln(out, "  NOTE: the job must have AWS credentials via a context or project env vars.")
		}
	} else {
		fmt.Fprintf(out, "  Storage: %s  (from --storage)\n", res.EncOpts.storage)
	}

	// ── Step 6: Artifact retention ────────────────────────────────────────────
	printStepHeader(out, 6, 8, "Artifact retention (security)")
	fmt.Fprintln(out, "  Setting retention to 1 day minimises how long secrets linger in artifacts.")
	fmt.Fprintln(out, "  NOTE: this lowers the ENTIRE ORG's artifact retention, not just this job.")
	fmt.Fprintln(out, "  The prior value is logged so you can restore it manually afterwards.")

	if res.ArtifactRetentionDays == 0 {
		setRetention, retErr := p.askBool("Set artifact retention to 1 day (recommended minimum)?", true)
		if retErr != nil {
			return res, retErr
		}
		if setRetention {
			res.ArtifactRetentionDays = 1
		}
	} else {
		fmt.Fprintf(out, "  Artifact retention: %d day(s)  (from --artifact-retention-days)\n", res.ArtifactRetentionDays)
	}

	// ── Step 7: Output path and branch ───────────────────────────────────────
	printStepHeader(out, 7, 8, "Output path and branch")
	fmt.Fprintln(out, "  Choose where to write the local secrets bundle and which branch to run on.")

	if res.Output == "" {
		outputVal, outErr := p.askWithDefault("Output bundle path", "secrets.json")
		if outErr != nil {
			return res, outErr
		}
		if outputVal == "" {
			outputVal = "secrets.json"
		}
		res.Output = outputVal
	} else {
		fmt.Fprintf(out, "  Output bundle: %s  (from --output)\n", res.Output)
	}

	if res.Branch == "" {
		branchVal, brErr := p.askWithDefault("Branch for the extraction run", "main")
		if brErr != nil {
			return res, brErr
		}
		if branchVal == "" {
			branchVal = "main"
		}
		res.Branch = branchVal
	} else {
		fmt.Fprintf(out, "  Branch: %s  (from --branch)\n", res.Branch)
	}

	// ── Step 8: Enable trigger ────────────────────────────────────────────────
	printStepHeader(out, 8, 8, "Enable api-trigger-with-config")
	fmt.Fprintln(out, "  The extraction pipeline uses an inline (unversioned) config trigger.")
	fmt.Fprintln(out, "  If the project does not have api-trigger-with-config enabled, the CLI can")
	fmt.Fprintln(out, "  enable it temporarily and restore the original setting after capture.")

	if !res.EnableTrigger {
		doEnable, enErr := p.askBool(
			"Enable api-trigger-with-config automatically if needed (and restore after)?",
			true,
		)
		if enErr != nil {
			return res, enErr
		}
		res.EnableTrigger = doEnable
	} else {
		fmt.Fprintln(out, "  Enable trigger: yes  (from --enable-trigger)")
	}

	// ── Confirmation summary ──────────────────────────────────────────────────
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "──────────────────────────────────────────────────────────────")
	fmt.Fprintln(out, "SUMMARY — review before running:")
	fmt.Fprintf(out, "  Manifest:            %s\n", res.ManifestPath)
	fmt.Fprintf(out, "  Output bundle:       %s\n", res.Output)
	if len(res.ContextNames) > 0 {
		fmt.Fprintf(out, "  Contexts:            %v\n", res.ContextNames)
		fmt.Fprintf(out, "  Host project:        %s\n", res.HostProjectSlug)
	}
	if len(res.ProjectSlugs) > 0 {
		fmt.Fprintf(out, "  Projects:            %v\n", res.ProjectSlugs)
	}
	if res.EncOpts.encrypt {
		if res.EncOpts.generateKey {
			fmt.Fprintln(out, "  Encryption:          age (generate-key)")
		} else {
			fmt.Fprintf(out, "  Encryption:          age (public key: %s)\n", res.EncOpts.sshPublicKey)
		}
	} else {
		fmt.Fprintln(out, "  Encryption:          NONE (plaintext artifact)")
	}
	fmt.Fprintf(out, "  Storage:             %s\n", func() string {
		if res.EncOpts.storage == "" {
			return "artifact"
		}
		return res.EncOpts.storage
	}())
	fmt.Fprintf(out, "  Branch:              %s\n", res.Branch)
	fmt.Fprintf(out, "  Enable trigger:      %v\n", res.EnableTrigger)
	if res.ArtifactRetentionDays > 0 {
		fmt.Fprintf(out, "  Artifact retention:  %d day(s)\n", res.ArtifactRetentionDays)
	}
	fmt.Fprintln(out, "──────────────────────────────────────────────────────────────")
	fmt.Fprintln(out, "")

	confirmed, confErr := p.askBool("Proceed with capture?", true)
	if confErr != nil {
		return res, confErr
	}
	if !confirmed {
		return res, fmt.Errorf("capture cancelled by user")
	}

	return res, nil
}
