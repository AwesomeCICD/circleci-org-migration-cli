// Package cmd implements the circleci-migrate command tree.
// The structure mirrors github.com/CircleCI-Public/circleci-cli/cmd so that
// merging the two codebases in the future requires minimal adaptation.
package cmd

import (
	"os"

	"github.com/CircleCI-Public/circleci-org-migration-cli/settings"
	"github.com/spf13/cobra"
)

// rootOptions holds the runtime configuration shared across all sub-commands.
var rootOptions *settings.Config

// Execute is the entry point called by main.  It builds the command tree,
// then delegates to cobra's Execute method.
func Execute() error {
	return MakeCommands().Execute()
}

// MakeCommands builds and returns the root cobra.Command with all
// sub-commands registered.  It is a standalone constructor so that tests and
// other callers can obtain a fresh command tree without side-effects.
func MakeCommands() *cobra.Command {
	rootOptions = &settings.Config{
		Host:         settings.DefaultHost,
		RestEndpoint: settings.DefaultRestEndpoint,
	}

	// Seed Host / Token from environment before flags are parsed so that env
	// vars act as defaults that CLI flags can override.
	if h := os.Getenv("CIRCLECI_HOST"); h != "" {
		rootOptions.Host = h
	}
	if t := os.Getenv("CIRCLECI_CLI_TOKEN"); t != "" {
		rootOptions.Token = t
	}
	if st := os.Getenv("CIRCLECI_SOURCE_TOKEN"); st != "" {
		rootOptions.SourceToken = st
	}
	if dt := os.Getenv("CIRCLECI_DEST_TOKEN"); dt != "" {
		rootOptions.DestToken = dt
	}

	rootCmd := &cobra.Command{
		Use:   "circleci-migrate",
		Short: "Migrate data between CircleCI organisations.",
		Long: `circleci-migrate helps you move data — contexts, project settings,
environment variables, and more — from one CircleCI organisation to another.

Typical workflow:

  1. Export the source organisation to a local manifest file:
       circleci-migrate export --source-token $SRC_TOKEN --org myorg > manifest.json

  2. (Optional) Review or edit the manifest.

  3. Sync the manifest into the destination organisation:
       circleci-migrate sync  --dest-token $DST_TOKEN --org neworg manifest.json

  Or run both steps in one shot:
       circleci-migrate migrate --source-token $SRC_TOKEN --dest-token $DST_TOKEN \
         --source-org myorg --dest-org neworg

Use "circleci-migrate [command] --help" for more information about a command.`,
		SilenceUsage: true,
	}

	// Persistent (global) flags — available to every sub-command.
	pf := rootCmd.PersistentFlags()
	pf.StringVar(&rootOptions.Host, "host", rootOptions.Host,
		"CircleCI host URL (env: CIRCLECI_HOST)")
	pf.StringVar(&rootOptions.Token, "token", rootOptions.Token,
		"Personal API token — fallback for both orgs (env: CIRCLECI_CLI_TOKEN)")
	pf.StringVar(&rootOptions.SourceToken, "source-token", rootOptions.SourceToken,
		"API token for the source org (env: CIRCLECI_SOURCE_TOKEN)")
	pf.StringVar(&rootOptions.DestToken, "dest-token", rootOptions.DestToken,
		"API token for the destination org (env: CIRCLECI_DEST_TOKEN)")
	pf.BoolVar(&rootOptions.Debug, "debug", false,
		"Enable debug logging")

	// Register sub-commands.
	rootCmd.AddCommand(newVersionCommand())
	rootCmd.AddCommand(newExportCommand())
	rootCmd.AddCommand(newSyncCommand())
	rootCmd.AddCommand(newMigrateCommand())

	return rootCmd
}
