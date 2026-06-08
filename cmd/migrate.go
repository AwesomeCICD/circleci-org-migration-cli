package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newMigrateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "All-in-one: export source org and sync into destination org.",
		Long: `migrate combines 'export' and 'sync' into a single command.

It reads data from the source CircleCI organisation, builds an in-memory
manifest, and immediately applies it to the destination organisation —
without writing a manifest file to disk.

Use this command for straightforward migrations where you do not need to
inspect or edit the manifest between the export and sync steps.

For more control (e.g. dry-run, review, or partial apply) run 'export' and
'sync' separately.

Example:
  circleci-migrate migrate \
    --source-token $SRC_TOKEN --source-org myorg \
    --dest-token   $DST_TOKEN --dest-org   neworg`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(cmd.OutOrStdout(),
				"This command is not implemented yet (coming in milestone M3).")
			return nil
		},
	}
}
