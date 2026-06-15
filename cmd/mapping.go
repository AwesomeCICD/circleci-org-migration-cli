package cmd

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
	"github.com/spf13/cobra"
)

// newMappingCommand returns the `mapping` parent command group.
func newMappingCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mapping",
		Short: "Generate and manage project slug mapping files.",
		Long: `mapping provides utilities for creating the mapping.json file that tells
'sync' and 'secrets transfer' how source project slugs correspond to
destination project slugs.

The most common use is 'mapping generate', which auto-matches projects by
repo name (the last segment of the slug) so you don't have to hand-write
mapping.json for a standard org rename.`,
	}

	cmd.AddCommand(newMappingGenerateCommand())
	return cmd
}

// newMappingGenerateCommand returns the `mapping generate` subcommand.
func newMappingGenerateCommand() *cobra.Command {
	var (
		manifestPath string
		destOrgSlug  string
		outputPath   string
	)

	cmd := &cobra.Command{
		Use:   "generate --manifest <file> --dest-org <slug> -o <mapping.json>",
		Short: "Auto-generate a project slug mapping from a manifest and a destination org.",
		Long: `generate lists the projects already onboarded in the destination org and
matches them against the projects captured in the export manifest by repo
name (the last '/'-separated segment of the slug, e.g. "web" from
"gh/acme/web").

Output:
  • A mapping.json file ready for use with 'sync --mapping' and
    'secrets transfer --mapping'.
  • A human-readable report printed to stdout with three sections:
      matched       — source slug → dest slug (written to mapping.json)
      unmatched     — source projects with no matching dest project
      dest-only     — dest projects with no source counterpart (info only)

Exit code is 0 even when there are unmatched entries — the report is the
deliverable; unmatched entries mean the user must onboard those projects
in the destination org first (or add manual entries to the mapping file).

Examples:
  circleci-migrate mapping generate \
    --manifest manifest.json \
    --dest-org gh/new-org \
    -o mapping.json

  circleci-migrate mapping generate \
    --manifest manifest.json \
    --dest-org circleci/aaaabbbb-cccc-dddd-eeee-ffffgggghhhh \
    --dest-token $CIRCLECI_DEST_TOKEN \
    -o mapping.json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg := configFromContext(ctx)

			// ── Validate required flags ──────────────────────────────────────
			if manifestPath == "" {
				return fmt.Errorf("--manifest is required")
			}
			if destOrgSlug == "" {
				return fmt.Errorf("--dest-org is required (e.g. --dest-org gh/new-org)")
			}

			// ── Token: prefer dest-specific, fall back to shared ─────────────
			// Uses the same precedence as sync/migrate: dest-specific token
			// first, then the shared --token / $CIRCLECI_CLI_TOKEN fallback.
			token := cfg.DestTokenOrDefault()
			if token == "" {
				return noDestTokenError()
			}

			// ── Load manifest ────────────────────────────────────────────────
			m, err := manifest.Load(manifestPath)
			if err != nil {
				return fmt.Errorf("loading manifest: %w", err)
			}

			// Collect source slugs from the manifest.
			srcSlugs := make([]string, 0, len(m.Projects))
			for _, p := range m.Projects {
				srcSlugs = append(srcSlugs, p.Slug)
			}

			// ── Resolve dest org ID ──────────────────────────────────────────
			orgClient, err := org.NewClient(cfg, token)
			if err != nil {
				return fmt.Errorf("creating org client: %w", err)
			}

			orgID, err := orgClient.ResolveOrgID(ctx, destOrgSlug)
			if err != nil {
				return fmt.Errorf("resolving destination org %q: %w", destOrgSlug, err)
			}

			// ── List dest org's onboarded projects ───────────────────────────
			projClient, err := project.NewClient(cfg, token)
			if err != nil {
				return fmt.Errorf("creating project client: %w", err)
			}

			destProjects, err := projClient.ListOrgProjects(ctx, orgID)
			if err != nil {
				return fmt.Errorf("listing destination org projects: %w", err)
			}

			// ── Match by repo name ───────────────────────────────────────────
			matched, unmatchedSrc, destOnly := matchProjects(srcSlugs, destProjects)

			// ── Write the mapping file ───────────────────────────────────────
			// Derive the org mapping from the first matched pair (informational
			// only — ResolveProjectSlug will use the explicit Projects map).
			orgFrom := m.Source.Org.Slug
			mp := &manifest.Mapping{
				Org: manifest.OrgMapping{
					From: orgFrom,
					To:   destOrgSlug,
				},
				Projects: matched,
			}

			// outputPath defaults to a sibling of the manifest so the user
			// gets a sensible default without thinking about paths.
			out := outputPath
			if out == "" {
				out = filepath.Join(filepath.Dir(manifestPath), "mapping.json")
			}

			if err := mp.Save(out); err != nil {
				return fmt.Errorf("writing mapping file %s: %w", out, err)
			}

			// ── Print human report ───────────────────────────────────────────
			w := cmd.OutOrStdout()

			// Sort for deterministic output.
			matchedSrc := make([]string, 0, len(matched))
			for s := range matched {
				matchedSrc = append(matchedSrc, s)
			}
			sort.Strings(matchedSrc)
			sort.Strings(unmatchedSrc)
			sort.Strings(destOnly)

			fmt.Fprintf(w, "Mapping written to: %s\n\n", out)

			fmt.Fprintf(w, "Matched (%d):\n", len(matched))
			if len(matched) == 0 {
				fmt.Fprintln(w, "  (none)")
			}
			for _, src := range matchedSrc {
				fmt.Fprintf(w, "  %s -> %s\n", src, matched[src])
			}

			fmt.Fprintln(w)
			fmt.Fprintf(w, "Unmatched source projects (%d):\n", len(unmatchedSrc))
			if len(unmatchedSrc) == 0 {
				fmt.Fprintln(w, "  (none)")
			}
			for _, src := range unmatchedSrc {
				repo := repoName(src)
				fmt.Fprintf(w, "  %s\n", src)
				fmt.Fprintf(w, "    → Onboard %q in the destination org first, or add a manual entry to %s.\n", repo, out)
			}

			fmt.Fprintln(w)
			fmt.Fprintf(w, "Destination-only projects (%d, no source counterpart — informational):\n", len(destOnly))
			if len(destOnly) == 0 {
				fmt.Fprintln(w, "  (none)")
			}
			for _, slug := range destOnly {
				fmt.Fprintf(w, "  %s\n", slug)
			}

			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&manifestPath, "manifest", "", "Path to the export manifest (required)")
	f.StringVar(&destOrgSlug, "dest-org", "",
		"Destination org slug, e.g. gh/new-org or circleci/<org-id> (required)")
	f.StringVarP(&outputPath, "output", "o", "",
		"Path to write the mapping file (default: mapping.json next to the manifest)")

	return cmd
}

// matchProjects matches source project slugs against destination OrgProject
// entries by project name.
//
// For source slugs the "name" is the last '/'-separated segment of the slug,
// which equals the repo/project name for both GitHub OAuth slugs
// ("gh/org/web" → "web") and GitHub App slugs ("circleci/uuid/uuid" — but
// source orgs always use OAuth-style slugs in the manifest).
//
// For destination projects the OrgProject.Name field is used directly.  This
// correctly handles GitHub App destination orgs where the slug's last segment
// is a project UUID rather than a human-readable name.
//
// Returns:
//
//	matched       map[srcSlug]destSlug — pairs where names are equal.
//	unmatchedSrc  source slugs that have no matching dest project.
//	destOnly      dest slugs that have no matching source project.
//
// This is a pure function with no network calls, making it straightforward to
// unit-test without httptest infrastructure.
func matchProjects(
	srcSlugs []string,
	destProjects []project.OrgProject,
) (matched map[string]string, unmatchedSrc []string, destOnly []string) {
	// Index dest projects by Name for O(1) lookup.  Name is the human-readable
	// project/repo name and is reliable for both OAuth and App orgs.
	destByName := make(map[string]project.OrgProject, len(destProjects))
	for _, dp := range destProjects {
		if dp.Name == "" {
			continue
		}
		// First occurrence wins if there are duplicate project names.
		if _, exists := destByName[dp.Name]; !exists {
			destByName[dp.Name] = dp
		}
	}

	matched = make(map[string]string)
	usedDestSlugs := make(map[string]bool)

	for _, src := range srcSlugs {
		// Use the last segment of the source slug as the repo name.
		name := repoName(src)
		if dp, ok := destByName[name]; ok {
			matched[src] = dp.Slug
			usedDestSlugs[dp.Slug] = true
		} else {
			unmatchedSrc = append(unmatchedSrc, src)
		}
	}

	// Collect dest-only: dest projects not matched to any source.
	for _, dp := range destProjects {
		if !usedDestSlugs[dp.Slug] {
			destOnly = append(destOnly, dp.Slug)
		}
	}

	return matched, unmatchedSrc, destOnly
}

// repoName returns the last '/'-separated segment of slug, which is the repo
// name portion of a CircleCI project slug (e.g. "web" from "gh/acme/web" or
// "gh/new-org/web").  Returns the whole slug if it contains no slash.
func repoName(slug string) string {
	if idx := strings.LastIndex(slug, "/"); idx >= 0 {
		return slug[idx+1:]
	}
	return slug
}
