package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newSyncCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "sync <manifest>",
		Short: "Apply a manifest file to the destination org.",
		Long: `sync reads a manifest produced by 'export' and writes its contents into
the destination CircleCI organisation.

Resources created or updated:
  - Contexts and their environment variables
  - Project-level environment variables
  - Project settings and metadata

sync is idempotent: running it multiple times produces the same result.
Existing resources in the destination org are updated to match the manifest;
resources not present in the manifest are left untouched.

Example:
  circleci-migrate sync --dest-token $DST_TOKEN --org neworg manifest.json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(cmd.OutOrStdout(),
				"This command is not implemented yet (coming in milestone M2).")
			return nil
		},
	}
}
