package cmd

import (
	"fmt"
	"os"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/clog"
	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

// newGenDocsCommand returns a hidden cobra command that regenerates the
// man pages and markdown CLI reference from the live command tree.
//
// Usage:
//
//	circleci-migrate gen-docs [--man-dir man] [--md-dir docs/cli]
//
// The command is hidden (Hidden: true) so it does not appear in --help or
// completions output, but it is still runnable by developers and the Makefile.
func newGenDocsCommand() *cobra.Command {
	var manDir string
	var mdDir string

	cmd := &cobra.Command{
		Use:    "gen-docs",
		Short:  "Generate man pages and markdown CLI reference (developer tool).",
		Hidden: true,
		Long: `gen-docs writes man pages and a markdown CLI reference for every command
in the circleci-migrate command tree.

Man pages are written to --man-dir (default: man/) in nroff format (.1).
Markdown pages are written to --md-dir (default: docs/cli/) as .md files.

Both output directories are created if they do not already exist.

This command is hidden — it does not appear in --help or shell completions.
It is called by 'make docs' and 'make man' during development.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root := cmd.Root()

			// Create output directories.
			for _, dir := range []string{manDir, mdDir} {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return fmt.Errorf("creating directory %q: %w", dir, err)
				}
			}

			// Generate man pages.
			clog.Infof("Generating man pages → %s", manDir)
			header := &doc.GenManHeader{
				Title:   "CIRCLECI-MIGRATE",
				Section: "1",
				Source:  "circleci-migrate",
				Manual:  "CircleCI Org Migration CLI",
			}
			if err := doc.GenManTree(root, header, manDir); err != nil {
				return fmt.Errorf("generating man pages: %w", err)
			}

			// Generate markdown reference.
			clog.Infof("Generating markdown reference → %s", mdDir)
			if err := doc.GenMarkdownTree(root, mdDir); err != nil {
				return fmt.Errorf("generating markdown reference: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Man pages written to      %s\n", manDir)
			fmt.Fprintf(cmd.OutOrStdout(), "Markdown reference to     %s\n", mdDir)
			return nil
		},
	}

	cmd.Flags().StringVar(&manDir, "man-dir", "man", "Directory to write man pages into")
	cmd.Flags().StringVar(&mdDir, "md-dir", "docs/cli", "Directory to write markdown reference into")

	return cmd
}
