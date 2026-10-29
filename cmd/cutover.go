package cmd

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/syncer"
	"github.com/spf13/cobra"
)

// cutoverStateDefault is the default --state file name.
const cutoverStateDefault = "cutover-state.json"

func newCutoverCommand() *cobra.Command {
	var (
		manifestPath string
		mappingPath  string
		statePath    string
		apply        bool
		pauseSource  bool
		pauseOrg     bool
		enableDest   bool
		undo         bool
	)

	cmd := &cobra.Command{
		Use:   "cutover [--pause-source] [--pause-org] [--enable-dest] [--undo] --manifest <file> [--apply]",
		Short: "Switch builds from the source org to the destination (pause source, enable destination, undo).",
		Long: `cutover performs the final switch of a migration: it stops builds in the SOURCE
org and turns builds on in the DESTINATION org, recording every change so it can
be reversed. It is a DRY RUN unless --apply is given.

  --pause-source   For each source project, pause it reversibly: set the v1.1
                   feature flag drop-all-build-requests=true on OAuth/VCS
                   projects (the prior value is recorded), and disable the
                   source triggers that are enabled and are not github_oauth
                   triggers (the ids are recorded). github_oauth triggers cannot
                   be disabled through the API, so OAuth projects rely on the
                   flag. Projects are NEVER unfollowed (that is destructive).
  --pause-org      Also set drop-all-build-requests=true on the whole source org
                   (the org-level flag overrides every project). Use with
                   --pause-source.
  --enable-dest    Enable the destination triggers whose source counterpart was
                   enabled (triggers are matched exactly as sync matches them).
                   A trigger that was disabled in the source stays disabled.
                   Run sync --apply first so the destination triggers exist.
  --undo           Restore everything recorded in the state file: source flags
                   back to their prior values, the source triggers it disabled
                   re-enabled, and the destination triggers it enabled
                   disabled again. Needs only --state (and --apply to act).

Every change is written to the state file (--state, default ` + cutoverStateDefault + `, mode 0600)
BEFORE it is made, so --undo works even after a partial failure. Re-running is
idempotent: items already in the wanted state report "exists". Each item is
printed with its outcome and the command exits non-zero if any item failed.

Order for a clean cutover: sync --apply (destination created paused), validate,
cutover --pause-source --apply, cutover --enable-dest --apply (the two can be
given together: the source is paused first). If something is wrong, cutover
--undo --apply returns the source to its previous state.

CAUTION: once the GitHub App is UNINSTALLED from the source GitHub org, the
source's GitHub App triggers no longer exist, so they can neither be disabled
nor re-enabled; pausing then relies on drop-all-build-requests alone, and --undo
can only restore that flag. Pause the source BEFORE uninstalling the App.

Source actions use the source token (--source-token / CIRCLECI_SOURCE_TOKEN,
else --token); destination actions use the destination token (--dest-token /
CIRCLECI_DEST_TOKEN, else --token).

Examples:
  circleci-migrate cutover --manifest manifest.json --pause-source
  circleci-migrate cutover --manifest manifest.json --mapping mapping.json --pause-source --enable-dest --apply
  circleci-migrate cutover --manifest manifest.json --pause-source --pause-org --apply
  circleci-migrate cutover --undo --apply`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg := configFromContext(ctx)
			errW := cmd.ErrOrStderr()

			if undo && (pauseSource || pauseOrg || enableDest) {
				return fmt.Errorf("--undo cannot be combined with --pause-source, --pause-org or --enable-dest")
			}
			if !undo && !pauseSource && !pauseOrg && !enableDest {
				return fmt.Errorf("nothing to do: pass --pause-source, --pause-org, --enable-dest or --undo")
			}
			if pauseOrg && !pauseSource {
				return fmt.Errorf("--pause-org is used together with --pause-source")
			}
			if statePath == "" {
				return fmt.Errorf("--state must not be empty")
			}

			var (
				m       *manifest.Manifest
				mapping *manifest.Mapping
				err     error
			)
			if !undo {
				if manifestPath == "" {
					return fmt.Errorf("--manifest is required")
				}
				if m, err = manifest.Load(manifestPath); err != nil {
					return err
				}
				if mappingPath != "" {
					if mapping, err = manifest.LoadMapping(mappingPath); err != nil {
						return err
					}
				}
				if enableDest && (mapping == nil || mapping.Org.To == "") {
					return fmt.Errorf("--enable-dest needs the destination org: pass --mapping with org.to set")
				}
			}

			c := &syncer.Cutover{}
			needSource := undo || pauseSource || pauseOrg
			needDest := undo || enableDest
			if needSource {
				token := cfg.SourceTokenOrDefault()
				if token == "" {
					return noSourceTokenError()
				}
				pc, perr := project.NewClient(cfg, token)
				if perr != nil {
					return fmt.Errorf("creating source project client: %w", perr)
				}
				oc, oerr := org.NewClient(cfg, token)
				if oerr != nil {
					return fmt.Errorf("creating source org client: %w", oerr)
				}
				c.Source, c.SourceOrg = pc, oc
			}
			if needDest {
				token := cfg.DestTokenOrDefault()
				if token == "" {
					return fmt.Errorf("no destination API token: set --dest-token, --token, CIRCLECI_DEST_TOKEN, or CIRCLECI_CLI_TOKEN")
				}
				sy, serr := buildSyncer(cfg, token, errW, false, false)
				if serr != nil {
					return serr
				}
				c.Dest = sy
				if pc, perr := project.NewClient(cfg, token); perr == nil {
					c.DestToggle = pc
				}
			}

			mode := "DRY RUN (nothing changed; re-run with --apply)"
			if apply {
				mode = "APPLY"
			}
			fmt.Fprintf(errW, "Cutover: %s — %s\n", describeCutover(pauseSource, pauseOrg, enableDest, undo), mode)
			if pauseSource || pauseOrg {
				fmt.Fprintf(errW, "Note: once the GitHub App is uninstalled from the source, its App triggers no longer exist; pausing then relies on drop-all-build-requests alone. Pause BEFORE uninstalling.\n")
			}

			rep, runErr := c.Run(ctx, m, mapping, syncer.CutoverOptions{
				Apply:       apply,
				PauseSource: pauseSource,
				PauseOrg:    pauseOrg,
				EnableDest:  enableDest,
				Undo:        undo,
				StatePath:   statePath,
			})
			if rep != nil {
				printCutoverReport(cmd.OutOrStdout(), rep, apply)
			}
			if runErr != nil {
				return runErr
			}
			if apply {
				fmt.Fprintf(errW, "State file: %s (mode 0600) — keep it until you are sure; 'cutover --undo --apply' restores from it.\n", statePath)
			}
			if n := rep.Counts()["error"]; n > 0 {
				return fmt.Errorf("%d cutover item(s) failed — see the [error] lines above, fix the cause and re-run (re-runs are idempotent)", n)
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&manifestPath, "manifest", "", "Path to the export manifest (required unless --undo)")
	f.StringVar(&mappingPath, "mapping", "", "Path to the source->destination mapping file (JSON); org.to is required with --enable-dest")
	f.StringVar(&statePath, "state", cutoverStateDefault, "State file recording every change (mode 0600); read by --undo")
	f.BoolVar(&apply, "apply", false, "Make the changes (default: dry run)")
	f.BoolVar(&pauseSource, "pause-source", false, "Pause the source: drop-all-build-requests on OAuth projects and disable enabled source App triggers")
	f.BoolVar(&pauseOrg, "pause-org", false, "Also set drop-all-build-requests on the whole source org (with --pause-source)")
	f.BoolVar(&enableDest, "enable-dest", false, "Enable destination triggers whose source counterpart was enabled")
	f.BoolVar(&undo, "undo", false, "Restore everything recorded in the state file")
	return cmd
}

func describeCutover(pauseSource, pauseOrg, enableDest, undo bool) string {
	if undo {
		return "undo"
	}
	var parts []string
	if pauseSource {
		parts = append(parts, "pause source")
	}
	if pauseOrg {
		parts = append(parts, "pause source org")
	}
	if enableDest {
		parts = append(parts, "enable destination")
	}
	return strings.Join(parts, " + ")
}

// printCutoverReport prints every action, grouped by kind, one line each.
func printCutoverReport(w io.Writer, rep *syncer.Report, apply bool) {
	byKind := map[string][]syncer.Action{}
	var kinds []string
	for _, a := range rep.Actions {
		if _, ok := byKind[a.Kind]; !ok {
			kinds = append(kinds, a.Kind)
		}
		byKind[a.Kind] = append(byKind[a.Kind], a)
	}
	sort.Strings(kinds)
	titles := map[string]string{
		"cutover-source-flag":    "Source projects (drop-all-build-requests)",
		"cutover-source-trigger": "Source triggers",
		"cutover-org-flag":       "Source org",
		"cutover-dest-trigger":   "Destination triggers",
		"cutover-state":          "State file",
	}
	for _, k := range kinds {
		title := titles[k]
		if title == "" {
			title = k
		}
		fmt.Fprintf(w, "\n%s\n", title)
		for _, a := range byKind[k] {
			fmt.Fprintf(w, "  [%s] %s — %s\n", a.Status, a.Target, a.Detail)
		}
	}
	c := rep.Counts()
	verb := "would change"
	if apply {
		verb = "changed"
	}
	fmt.Fprintf(w, "\n%s: %d, already as wanted: %d, skipped: %d, manual: %d, errors: %d\n", verb, c["set"], c["exists"], c["skipped"], c["manual"], c["error"])
}
