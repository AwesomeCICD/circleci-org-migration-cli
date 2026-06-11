// Package cmd implements the circleci-migrate command tree.
// The structure mirrors github.com/CircleCI-Public/circleci-cli/cmd so that
// merging the two codebases in the future requires minimal adaptation.
package cmd

import (
	"fmt"
	"net/url"
	"os"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/clog"
	"github.com/AwesomeCICD/circleci-org-migration-cli/settings"
	"github.com/spf13/cobra"
)

// rootOptions holds the runtime configuration shared across all sub-commands.
var rootOptions *settings.Config

// Execute is the entry point called by main.  It builds the command tree,
// then delegates to cobra's Execute method.
func Execute() error {
	return MakeCommands().Execute()
}

// resolveHost returns the best available CircleCI host from the environment.
// Priority order (highest to lowest):
//  1. CIRCLECI_CLI_HOST — the official circleci-cli variable
//  2. CIRCLECI_HOST — legacy alias
//  3. CIRCLE_URL — injected by `circleci run migrate`; only scheme+host is
//     used (path components, if any, are stripped) so a full server URL like
//     "https://circleci.com/api/..." works correctly.
//
// Returns empty string if none is set so the caller falls back to the
// compiled-in default.
func resolveHost() string {
	if h := os.Getenv("CIRCLECI_CLI_HOST"); h != "" {
		return h
	}
	if h := os.Getenv("CIRCLECI_HOST"); h != "" {
		return h
	}
	// CIRCLE_URL is injected by the official circleci-cli when invoked as
	// `circleci run migrate ...`. Parse it and keep only the scheme+host so
	// downstream code that constructs API paths against this value is not
	// confused by an unexpected path prefix.
	if raw := os.Getenv("CIRCLE_URL"); raw != "" {
		if u, err := url.Parse(raw); err == nil && u.Host != "" {
			return u.Scheme + "://" + u.Host
		}
		// If it has no scheme (e.g. "circleci.com"), return as-is so the
		// caller receives something usable rather than an empty string.
		return raw
	}
	return ""
}

// MakeCommands builds and returns the root cobra.Command with all
// sub-commands registered.  It is a standalone constructor so that tests and
// other callers can obtain a fresh command tree without side-effects.
func MakeCommands() *cobra.Command {
	rootOptions = &settings.Config{
		Host:         settings.DefaultHost,
		RestEndpoint: settings.DefaultRestEndpoint,
	}

	// Host may be seeded from the environment here because it is NOT sensitive:
	// seeding makes it the flag default shown in --help, which is fine for a URL.
	// Token env fallbacks are deliberately NOT seeded here — doing so would make
	// the secret value the flag's default and leak it into --help output. They
	// are resolved in PersistentPreRunE instead (after flag parsing).
	if h := resolveHost(); h != "" {
		rootOptions.Host = h
	}

	rootCmd := &cobra.Command{
		Use:   "circleci-migrate",
		Short: "Migrate data between CircleCI organisations.",
		Long: `circleci-migrate helps you move data — contexts, project settings,
environment variables, and more — from one CircleCI organisation to another.

Typical workflow:

  1. Export the source organisation to a local manifest file:
       circleci-migrate export --source-token $SRC_TOKEN --org gh/myorg

  2. (Optional) Review or edit the manifest.

  3. Sync the manifest into the destination organisation (the destination org is
     inferred from the manifest; no --org flag is needed):
       circleci-migrate sync  --dest-token $DST_TOKEN --manifest manifest.json --apply

  Or run both steps in one shot:
       circleci-migrate migrate --source-token $SRC_TOKEN --dest-token $DST_TOKEN \
         --source-org gh/myorg --dest-org gh/neworg

Use "circleci-migrate [command] --help" for more information about a command.`,
		SilenceUsage: true,
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			// Resolve token fallbacks from the environment AFTER flag parsing so
			// secret values never appear as flag defaults in --help. An explicit
			// flag always wins; otherwise fall back to the matching env var.
			//
			// Token precedence (highest → lowest):
			//   --token / --source-token / --dest-token (explicit flags)
			//   → CIRCLECI_CLI_TOKEN / CIRCLECI_SOURCE_TOKEN / CIRCLECI_DEST_TOKEN
			//   → CIRCLE_TOKEN (injected by `circleci run migrate`)
			if rootOptions.Token == "" {
				rootOptions.Token = os.Getenv("CIRCLECI_CLI_TOKEN")
			}
			if rootOptions.Token == "" {
				// CIRCLE_TOKEN is injected by the official circleci-cli when
				// invoked as `circleci run migrate ...`.
				rootOptions.Token = os.Getenv("CIRCLE_TOKEN")
			}
			if rootOptions.SourceToken == "" {
				rootOptions.SourceToken = os.Getenv("CIRCLECI_SOURCE_TOKEN")
			}
			if rootOptions.DestToken == "" {
				rootOptions.DestToken = os.Getenv("CIRCLECI_DEST_TOKEN")
			}

			level := clog.LevelInfo
			if rootOptions.Debug {
				level = clog.LevelDebug
			}
			clog.SetDefault(clog.New(os.Stderr, level))
			return nil
		},
	}

	// SetFlagErrorFunc provides a consistent error message + usage hint when a
	// flag cannot be parsed (e.g. unknown flag, wrong type).  Mirrors the
	// official circleci-cli behaviour so error output is uniform.
	rootCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return fmt.Errorf("%w\nRun '%s --help' for usage.", err, cmd.CommandPath())
	})

	// Persistent (global) flags — available to every sub-command.
	pf := rootCmd.PersistentFlags()
	pf.StringVar(&rootOptions.Host, "host", rootOptions.Host,
		"CircleCI host URL (env: CIRCLECI_CLI_HOST, CIRCLECI_HOST, or CIRCLE_URL)")
	// Token flags default to "" (never the env value) so --help never prints a
	// secret. The env fallback is applied in PersistentPreRunE.
	pf.StringVar(&rootOptions.Token, "token", "",
		"Personal API token — fallback for both orgs (env: CIRCLECI_CLI_TOKEN or CIRCLE_TOKEN)")
	pf.StringVar(&rootOptions.SourceToken, "source-token", "",
		"API token for the source org (env: CIRCLECI_SOURCE_TOKEN)")
	pf.StringVar(&rootOptions.DestToken, "dest-token", "",
		"API token for the destination org (env: CIRCLECI_DEST_TOKEN)")
	pf.BoolVar(&rootOptions.Debug, "debug", false,
		"Enable debug logging")

	// Register sub-commands.
	rootCmd.AddCommand(newVersionCommand())
	rootCmd.AddCommand(newExportCommand())
	rootCmd.AddCommand(newSecretsCommand())
	rootCmd.AddCommand(newSyncCommand())
	rootCmd.AddCommand(newMigrateCommand())
	rootCmd.AddCommand(newOrbCommand())
	// gen-docs is a hidden developer command — not shown in --help.
	rootCmd.AddCommand(newGenDocsCommand())
	// bundle-encrypt is a hidden internal command used by inline pipeline configs.
	rootCmd.AddCommand(newBundleEncryptCommand())

	return rootCmd
}
