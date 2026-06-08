package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newExportCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "export",
		Short: "Export source-org data to a local manifest file.",
		Long: `export reads configuration from the source CircleCI organisation and
writes it to a JSON manifest file on disk (or stdout).

The manifest captures:
  - Contexts and their environment variables
  - Project-level environment variables
  - Project settings and metadata
  - VCS integration configuration

This command is read-only with respect to CircleCI — it never writes to
either organisation.  Review the manifest before running 'sync'.

Example:
  circleci-migrate export --source-token $SRC_TOKEN --org myorg > manifest.json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(cmd.OutOrStdout(),
				"This command is not implemented yet (coming in milestone M1).")
			return nil
		},
	}
}
