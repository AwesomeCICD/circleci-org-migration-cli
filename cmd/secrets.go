package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"filippo.io/age"

	bundlepkg "github.com/AwesomeCICD/circleci-org-migration-cli/internal/bundle"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/clog"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/secrets"
	"github.com/AwesomeCICD/circleci-org-migration-cli/version"
	"github.com/spf13/cobra"
)

func newSecretsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Capture secret values that the API cannot expose (RECOMMENDED: use 'secrets capture').",
		Long: `secrets handles the one thing the CircleCI API cannot: secret VALUES.

CircleCI masks environment-variable values everywhere in its API, so 'export'
captures only their names. The 'secrets' subcommands recover those values by
running a pipeline inside CircleCI and reading the variables from the job env.

NOTE: 'secrets extract' is designed to run INSIDE a CircleCI job (not locally).
For the recommended local workflow, use 'secrets capture' instead.

RECOMMENDED PATH — 'secrets capture' (CLI-orchestrated, no committed config):

  circleci-migrate secrets capture

  Run on an interactive terminal to launch the guided walkthrough. The CLI:
    • Loads your manifest to list available contexts and projects.
    • Lets you pick which contexts and projects to extract.
    • Prompts for the HOST PROJECT under which context extraction runs
      (any project works; build history is irrelevant).
    • Recommends encryption (age) so plaintext never persists in artifacts.
    • Builds an inline unversioned pipeline config and triggers the run.
    • Polls until completion, then downloads and decrypts the artifact.
    • Writes the captured values to a local secret bundle.

  All flags bypass prompts for CI/scripted use — see 'secrets capture --help'.

ALTERNATIVE PATH — orb / 'secrets extract' (committed config):

  Use 'circleci-migrate orb inline' or the awesomecicd/circleci-org-migration
  orb to add an extraction job to an existing pipeline config. The in-job
  'secrets extract' command reads values from the job environment.

  This path requires committing a config change but gives you full control
  over when and how the extraction job runs.

Subcommands:
  capture   CLI-orchestrated extraction via unversioned pipeline (RECOMMENDED)
  extract   In-job extraction from the current environment (orb path)
  decrypt   Decrypt an age-encrypted secret bundle locally
  merge     Merge multiple secret bundles into one`,
	}
	cmd.AddCommand(newSecretsExtractCommand())
	cmd.AddCommand(newSecretsMergeCommand())
	cmd.AddCommand(newSecretsCaptureCommand())
	cmd.AddCommand(newSecretsDecryptCommand())
	return cmd
}

func newSecretsMergeCommand() *cobra.Command {
	var (
		output        string
		encryptFlag   bool
		recipientStr  string
		recipientFile string
	)

	cmd := &cobra.Command{
		Use:   "merge -o <out> <bundle.json>...",
		Short: "Merge multiple secret bundles into one.",
		Long: `merge combines several secret bundles (for example, the per-context
bundles produced by separate extraction jobs) into a single bundle.

When --encrypt is set the merged bundle is written as an age-encrypted file
(<output>.age) instead of plaintext. Provide the recipient's public key via
--recipient (inline) or --recipient-file (path to a .pub or age recipients
file). The corresponding private key is required to decrypt with
'secrets decrypt'.

Example:
  circleci-migrate secrets merge -o secrets.json artifacts/*/secrets.json
  circleci-migrate secrets merge --encrypt --recipient-file ~/.ssh/id_ed25519.pub \
    -o secrets.json artifacts/*/secrets.json`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("at least one <bundle.json> path is required — run '%s --help' for usage", cmd.CommandPath())
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			merged := manifest.NewSecretBundle()
			for _, path := range args {
				b, err := manifest.LoadSecretBundle(path)
				if err != nil {
					return err
				}
				merged.Merge(b)
			}
			merged.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
			merged.ToolVersion = version.UserAgent()

			if encryptFlag {
				outPath, encErr := encryptAndWriteBundle(merged, output, recipientStr, recipientFile)
				if encErr != nil {
					return encErr
				}
				ctxN, varN := 0, 0
				for _, vars := range merged.ContextSecrets {
					ctxN++
					varN += len(vars)
				}
				projN := 0
				for _, vars := range merged.ProjectSecrets {
					projN++
					varN += len(vars)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Merged %d bundle(s) → %s (%d context(s), %d project(s), %d value(s))\n",
					len(args), outPath, ctxN, projN, varN)
				fmt.Fprintf(cmd.ErrOrStderr(), "NOTE: %s is age-encrypted — use 'secrets decrypt' to access it.\n", outPath)
				return nil
			}

			if err := merged.Save(output); err != nil {
				return fmt.Errorf("writing merged bundle: %w", err)
			}

			ctxN, varN := 0, 0
			for _, vars := range merged.ContextSecrets {
				ctxN++
				varN += len(vars)
			}
			projN := 0
			for _, vars := range merged.ProjectSecrets {
				projN++
				varN += len(vars)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Merged %d bundle(s) → %s (%d context(s), %d project(s), %d value(s))\n",
				len(args), output, ctxN, projN, varN)
			fmt.Fprintln(cmd.ErrOrStderr(), "WARNING: "+output+" contains plaintext secrets — protect it and do not commit it.")
			return nil
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "secrets.json", "Path to write the merged bundle")
	cmd.Flags().BoolVar(&encryptFlag, "encrypt", false,
		"Encrypt the output bundle with age (writes <output>.age; requires --recipient or --recipient-file)")
	cmd.Flags().StringVar(&recipientStr, "recipient", "",
		"age or SSH public key recipient string (ssh-ed25519/ssh-rsa/age1...)")
	cmd.Flags().StringVar(&recipientFile, "recipient-file", "",
		"Path to an SSH public key (.pub) or age recipients file")
	return cmd
}

func newSecretsExtractCommand() *cobra.Command {
	var (
		manifestPath  string
		output        string
		contextName   string
		projectSlug   string
		strict        bool
		encryptFlag   bool
		recipientStr  string
		recipientFile string
	)

	cmd := &cobra.Command{
		Use:   "extract --manifest <file> (--context <name> | --project <slug>)",
		Short: "Extract secret values from the current job environment (for use in orb/pipeline jobs).",
		Long: `extract reads the variable names for one context or project from the
manifest, looks each value up in the current environment, and records the
found values in a secret bundle (merging into an existing bundle if present).

Run this inside a CircleCI job that injects the target's variables:
  - For a context, the job must reference exactly that context.
  - For project variables, the job must run within that project.

When --encrypt is set, the bundle is written as an age-encrypted file
(<output>.age) and the plaintext file is NOT written. Provide the recipient's
public key via --recipient (inline string) or --recipient-file (path to a .pub
or age recipients file).

Examples:
  circleci-migrate secrets extract --manifest manifest.json --context deploy-prod
  circleci-migrate secrets extract --manifest manifest.json --project gh/acme/web -o secrets.json
  circleci-migrate secrets extract --manifest manifest.json --context deploy-prod \
    --encrypt --recipient-file /tmp/migration.pub`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if manifestPath == "" {
				return errors.New("--manifest is required")
			}
			if (contextName == "") == (projectSlug == "") {
				return errors.New("exactly one of --context or --project is required")
			}

			m, err := manifest.Load(manifestPath)
			if err != nil {
				return err
			}
			bndl, err := loadOrNewBundle(output)
			if err != nil {
				return err
			}

			var (
				res    *secrets.Result
				target string
			)
			if contextName != "" {
				target = "context " + contextName
				res, err = secrets.ExtractContext(m, bndl, contextName, os.LookupEnv)
			} else {
				target = "project " + projectSlug
				res, err = secrets.ExtractProject(m, bndl, projectSlug, os.LookupEnv)
			}
			if err != nil {
				return err
			}

			bndl.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
			bndl.ToolVersion = version.UserAgent()

			if encryptFlag {
				outPath, encErr := encryptAndWriteBundle(bndl, output, recipientStr, recipientFile)
				if encErr != nil {
					return encErr
				}
				out := cmd.OutOrStdout()
				total := len(res.Found) + len(res.Missing)
				fmt.Fprintf(out, "Captured %d/%d variable(s) for %s into %s\n", len(res.Found), total, target, outPath)
				if len(res.Missing) > 0 {
					fmt.Fprintf(out, "Not found in this job's environment: %v\n", res.Missing)
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "NOTE: %s is age-encrypted — use 'secrets decrypt' to access it.\n", outPath)
			} else {
				if err := bndl.Save(output); err != nil {
					return fmt.Errorf("writing secret bundle: %w", err)
				}
				out := cmd.OutOrStdout()
				total := len(res.Found) + len(res.Missing)
				fmt.Fprintf(out, "Captured %d/%d variable(s) for %s into %s\n", len(res.Found), total, target, output)
				if len(res.Missing) > 0 {
					fmt.Fprintf(out, "Not found in this job's environment: %v\n", res.Missing)
				}
				fmt.Fprintln(cmd.ErrOrStderr(), "WARNING: "+output+" contains plaintext secrets — protect it and do not commit it.")
			}

			if strict && len(res.Missing) > 0 {
				return fmt.Errorf("%d variable(s) were missing from the environment", len(res.Missing))
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&manifestPath, "manifest", "", "Path to the export manifest (required)")
	f.StringVarP(&output, "output", "o", "secrets.json", "Path to the secret bundle to write/append")
	f.StringVar(&contextName, "context", "", "Context name to capture (mutually exclusive with --project)")
	f.StringVar(&projectSlug, "project", "", "Project slug to capture (mutually exclusive with --context)")
	f.BoolVar(&strict, "strict", false, "Fail if any expected variable is missing from the environment")
	f.BoolVar(&encryptFlag, "encrypt", false,
		"Encrypt the output bundle with age (writes <output>.age; requires --recipient or --recipient-file)")
	f.StringVar(&recipientStr, "recipient", "",
		"age or SSH public key recipient string (ssh-ed25519/ssh-rsa/age1...)")
	f.StringVar(&recipientFile, "recipient-file", "",
		"Path to an SSH public key (.pub) or age recipients file")

	return cmd
}

// newSecretsDecryptCommand builds the "secrets decrypt" subcommand.
func newSecretsDecryptCommand() *cobra.Command {
	var (
		identityFile string
		output       string
	)

	cmd := &cobra.Command{
		Use:   "decrypt --identity-file <key> [-o <out.json>] <bundle.age>",
		Short: "Decrypt an age-encrypted secret bundle.",
		Long: `decrypt decrypts an age-encrypted bundle produced by 'secrets extract --encrypt'
or 'secrets capture --encrypt' into a plaintext secrets.json file.

The --identity-file flag accepts either:
  - An SSH private key file (OpenSSH format, ed25519 or RSA)
  - An age identity file (AGE-SECRET-KEY-1...)

The decrypted bundle is written to --output (default: secrets.json).

SECURITY: The output file contains plaintext secrets. Protect it, do not
commit it to version control, and delete it once the sync is complete.

Examples:
  circleci-migrate secrets decrypt --identity-file ~/.ssh/id_ed25519 bundle.age
  circleci-migrate secrets decrypt --identity-file identity.age -o secrets.json bundle.age`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("<bundle.age> path is required — run '%s --help' for usage", cmd.CommandPath())
			}
			if len(args) > 1 {
				return fmt.Errorf("accepts 1 <bundle.age> path, received %d — run '%s --help' for usage", len(args), cmd.CommandPath())
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			inputPath := args[0]

			if identityFile == "" {
				return errors.New("--identity-file is required")
			}

			clog.Infof("reading encrypted bundle from %s", inputPath)

			ciphertext, err := os.ReadFile(inputPath)
			if err != nil {
				return fmt.Errorf("reading encrypted bundle %s: %w", inputPath, err)
			}

			// SECURITY: do not log identityFile contents.
			identities, err := bundlepkg.ParseIdentityFile(identityFile)
			if err != nil {
				return fmt.Errorf("loading identity: %w", err)
			}

			plaintext, err := bundlepkg.DecryptBundle(ciphertext, identities...)
			if err != nil {
				return fmt.Errorf("decrypting bundle: %w", err)
			}

			// Validate that the plaintext is a valid SecretBundle.
			var b manifest.SecretBundle
			if jsonErr := json.Unmarshal(plaintext, &b); jsonErr != nil {
				return fmt.Errorf("decrypted data is not a valid secret bundle: %w", jsonErr)
			}

			// Write with 0600 permissions — plaintext secrets.
			// #nosec G703 -- output is an operator-provided path on their own machine (local CLI), not attacker-controlled.
			if err := os.WriteFile(output, plaintext, 0o600); err != nil {
				return fmt.Errorf("writing decrypted bundle to %s: %w", output, err)
			}

			clog.Infof("plaintext bundle written to %s", output)
			fmt.Fprintf(cmd.OutOrStdout(), "Decrypted bundle written to %s\n", output)
			fmt.Fprintln(cmd.ErrOrStderr(), "WARNING: "+output+" contains plaintext secrets — protect it and do not commit it.")
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&identityFile, "identity-file", "",
		"Path to an SSH private key or age identity file for decryption (required)")
	f.StringVarP(&output, "output", "o", "secrets.json",
		"Path to write the decrypted bundle")
	return cmd
}

// loadOrNewBundle returns the existing bundle at path, or a fresh one if the
// file does not exist, so repeated extract calls accumulate into one bundle.
func loadOrNewBundle(path string) (*manifest.SecretBundle, error) {
	if _, err := os.Stat(path); err == nil {
		return manifest.LoadSecretBundle(path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return manifest.NewSecretBundle(), nil
}

// encryptAndWriteBundle marshals b to JSON, encrypts it with the specified
// recipient, and writes the age ciphertext to <basePath>.age.
// The plaintext is held only in memory and is never written to disk.
//
// Returns the path of the written encrypted file.
//
// SECURITY: the marshalled JSON contains plaintext secrets — never log it.
func encryptAndWriteBundle(
	b *manifest.SecretBundle,
	basePath string,
	recipientStr string,
	recipientFile string,
) (string, error) {
	if recipientStr == "" && recipientFile == "" {
		return "", errors.New("--encrypt requires --recipient or --recipient-file")
	}
	if recipientStr != "" && recipientFile != "" {
		return "", errors.New("specify only one of --recipient or --recipient-file, not both")
	}

	var (
		r   age.Recipient
		err error
	)
	if recipientStr != "" {
		r, err = bundlepkg.ParseRecipient(strings.TrimSpace(recipientStr))
	} else {
		r, err = bundlepkg.ParseRecipientFile(recipientFile)
	}
	if err != nil {
		return "", fmt.Errorf("parsing recipient: %w", err)
	}

	// Marshal the bundle to JSON. Do NOT log the bytes.
	plaintext, err := json.Marshal(b)
	if err != nil {
		return "", fmt.Errorf("marshalling bundle: %w", err)
	}

	ciphertext, err := bundlepkg.EncryptBundle(plaintext, r)
	if err != nil {
		return "", fmt.Errorf("encrypting bundle: %w", err)
	}

	outPath := strings.TrimSuffix(basePath, ".age") + ".age"
	if err := os.WriteFile(outPath, ciphertext, 0o600); err != nil {
		return "", fmt.Errorf("writing encrypted bundle to %s: %w", outPath, err)
	}

	clog.Infof("encrypted bundle written to %s (%d bytes)", outPath, len(ciphertext))
	return outPath, nil
}
