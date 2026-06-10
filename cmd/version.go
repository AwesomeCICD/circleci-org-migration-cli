package cmd

import (
	"fmt"
	"runtime"

	"github.com/AwesomeCICD/circleci-org-migration-cli/version"
	"github.com/spf13/cobra"
)

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Display version and build information.",
		Long:  "Prints the version number, git commit SHA, and OS/architecture of this build.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintf(cmd.OutOrStdout(),
				"circleci-migrate v%s (commit: %s, %s/%s)\n",
				version.Version,
				version.Commit,
				runtime.GOOS,
				runtime.GOARCH,
			)
			return nil
		},
	}
}
