// Package cmd implements the circleci-migrate command tree.
// The structure mirrors github.com/CircleCI-Public/circleci-cli/cmd so that
// merging the two codebases in the future requires minimal adaptation.
package cmd

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/rest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/clog"
	"github.com/AwesomeCICD/circleci-org-migration-cli/settings"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"golang.org/x/term"
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

// helpWidth returns a sensible terminal width for help rendering.
// Falls back to 120 when stdout is not a TTY (e.g. piped output).
func helpWidth() int {
	const defaultWidth = 120
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return defaultWidth
	}
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err == nil && w > 0 && w < defaultWidth {
		return w
	}
	return defaultWidth
}

// styledHelpFunc returns a cobra help function that renders help output with
// lipgloss styling, matching the visual style of the official circleci-cli.
// When stdout is not a TTY, lipgloss automatically strips ANSI escape codes so
// piped output (e.g. "--help | cat") is always clean plain text.
func styledHelpFunc(width int) func(*cobra.Command, []string) {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#003740", Dark: "#3B6385"}).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(lipgloss.AdaptiveColor{Light: "#003740", Dark: "#3B6385"}).
		Margin(1, 0, 0, 0).
		Padding(0, 1)

	subCmdNameStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#161616", Dark: "#FFFFFF"}).
		Padding(0, 4)

	subCmdDescStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#161616", Dark: "#FFFFFF"}).
		Bold(true)

	textStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#161616", Dark: "#FFFFFF"})

	_ = width // width is available for future border/wrap use

	return func(cmd *cobra.Command, _ []string) {
		var b strings.Builder

		// Command path + short description header.
		b.WriteString(titleStyle.Render(cmd.CommandPath()))
		b.WriteString("\n")

		// Long description, or short if no long description.
		desc := strings.TrimSpace(cmd.Long)
		if desc == "" {
			desc = strings.TrimSpace(cmd.Short)
		}
		if desc != "" {
			b.WriteString(textStyle.Render(desc))
			b.WriteString("\n")
		}

		// Aliases.
		if len(cmd.Aliases) > 0 {
			b.WriteString("\n")
			b.WriteString(titleStyle.Render("Aliases:"))
			b.WriteString("\n")
			b.WriteString(textStyle.Padding(0, 2).Render(cmd.NameAndAliases()))
			b.WriteString("\n")
		}

		// Usage line.
		if cmd.Runnable() {
			b.WriteString("\n")
			b.WriteString(titleStyle.Render("Usage:"))
			b.WriteString("\n")
			b.WriteString(textStyle.Padding(0, 2).Render(cmd.UseLine()))
			b.WriteString("\n")
		} else if cmd.HasAvailableSubCommands() {
			b.WriteString("\n")
			b.WriteString(titleStyle.Render("Usage:"))
			b.WriteString("\n")
			b.WriteString(textStyle.Padding(0, 2).Render(cmd.CommandPath() + " [command]"))
			b.WriteString("\n")
		}

		// Examples.
		if cmd.HasExample() {
			b.WriteString("\n")
			b.WriteString(titleStyle.Render("Examples:"))
			b.WriteString("\n")
			b.WriteString(textStyle.Render(cmd.Example))
			b.WriteString("\n")
		}

		// Available sub-commands.
		if cmd.HasAvailableSubCommands() {
			b.WriteString("\n")
			b.WriteString(titleStyle.Render("Available Commands:"))
			b.WriteString("\n")
			for _, sub := range cmd.Commands() {
				if !sub.IsAvailableCommand() {
					continue
				}
				namePart := subCmdNameStyle.Render(sub.Name())
				// Pad so descriptions line up; NamePadding accounts for the
				// longest sibling name.
				pad := sub.NamePadding() - len(sub.Name())
				if pad < 1 {
					pad = 1
				}
				descPart := subCmdDescStyle.PaddingLeft(pad).Render(sub.Short)
				b.WriteString(namePart + descPart + "\n")
			}
		}

		// Local flags.
		if cmd.HasAvailableLocalFlags() {
			b.WriteString("\n")
			b.WriteString(titleStyle.Render("Flags:"))
			b.WriteString("\n")
			b.WriteString(textStyle.Render(cmd.LocalFlags().FlagUsages()))
		}

		// Inherited (global) flags.
		if cmd.HasAvailableInheritedFlags() {
			b.WriteString("\n")
			b.WriteString(titleStyle.Render("Global Flags:"))
			b.WriteString("\n")
			b.WriteString(textStyle.Render(cmd.InheritedFlags().FlagUsages()))
		}

		// Footer hint.
		if cmd.HasAvailableSubCommands() {
			b.WriteString("\n")
			b.WriteString(textStyle.Faint(true).Render(
				fmt.Sprintf(`Use "%s [command] --help" for more information about a command.`, cmd.CommandPath()),
			))
			b.WriteString("\n")
		}

		// Write to the command's output writer so tests can capture it.
		fmt.Fprintln(cmd.OutOrStdout(), b.String())
	}
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
       circleci-migrate export --source-token $SRC_TOKEN --source-org gh/myorg

  2. (Optional) Review or edit the manifest.

  3. Sync the manifest into the destination organisation (the destination org is
     inferred from the manifest; no --source-org flag is needed):
       circleci-migrate sync  --dest-token $DST_TOKEN --manifest manifest.json --apply

  Or run both steps in one shot:
       circleci-migrate migrate --source-token $SRC_TOKEN --dest-token $DST_TOKEN \
         --source-org gh/myorg --dest-org gh/neworg

Use "circleci-migrate [command] --help" for more information about a command.`,
		SilenceUsage: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			// Forward the active sub-command path (e.g. "circleci-migrate export")
			// to the API via the Circleci-Cli-Command header on every REST client
			// constructed after this point, mirroring the official circleci-cli.
			rest.SetDefaultCommandPath(cmd.CommandPath())

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

	// Hide the auto-generated completion command so it does not appear in
	// --help output (it still works when invoked directly).
	rootCmd.CompletionOptions.HiddenDefaultCmd = true

	// Apply the lipgloss-styled help function to the root command.
	// All sub-commands inherit this via cobra's help propagation.
	rootCmd.SetHelpFunc(styledHelpFunc(helpWidth()))

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

	// Enforce SilenceUsage on the whole command tree in one place so that no
	// individual sub-command (existing or future) can forget it: on a RunE error
	// cobra would otherwise dump the full usage text, burying the error message.
	silenceUsageTree(rootCmd)

	return rootCmd
}

// silenceUsageTree sets SilenceUsage = true on cmd and every descendant. It is
// the single source of truth for this setting so adding a new sub-command can
// never accidentally reintroduce usage-on-error noise.
func silenceUsageTree(cmd *cobra.Command) {
	cmd.SilenceUsage = true
	for _, child := range cmd.Commands() {
		silenceUsageTree(child)
	}
}
