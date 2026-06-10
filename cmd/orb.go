package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/orbsource"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// fetchOrbSource is the function used to retrieve orb YAML source.  It is a
// package-level variable so that tests can substitute a fake implementation
// without making real HTTP requests.
var fetchOrbSource = orbsource.FetchOrbSource

// FetchOrbSourceFunc is the function signature for orb source retrieval.
type FetchOrbSourceFunc func(host, token, orbRef string) (string, error)

// ExposedFetchOrbSource returns the current fetchOrbSource function.
// It is exported for use in tests (package cmd_test).
func ExposedFetchOrbSource() FetchOrbSourceFunc {
	return fetchOrbSource
}

// SetFetchOrbSource replaces the fetchOrbSource function.
// It is exported for use in tests (package cmd_test).
func SetFetchOrbSource(f FetchOrbSourceFunc) {
	fetchOrbSource = f
}

// orbKeysToKeep are the top-level orb source keys that are retained when
// inlining; keys like "version", "description", and "display" are metadata
// that CircleCI requires in a standalone orb definition but that must NOT
// appear inside an inline orb.
var orbKeysToKeep = map[string]bool{
	"commands":   true,
	"jobs":       true,
	"executors":  true,
	"parameters": true,
}

// newOrbCommand returns the parent "orb" cobra.Command with its sub-commands
// registered.
func newOrbCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "orb",
		Short: "Manage CircleCI orb references in pipeline configs.",
		Long: `The orb sub-commands help you work with orb references in
.circleci/config.yml files during an organisation migration.

During a migration overlap window a private orb's namespace lives in only one
organisation, so repos that have moved to the destination org cannot resolve
orbs from the source org's namespace.  The "inline" sub-command works around
this by fetching the orb's published source and embedding it directly inside
the consuming config, removing the namespace dependency.  After the namespace
is transferred the change should be reverted.`,
		SilenceUsage: true,
	}

	cmd.AddCommand(newOrbInlineCommand())
	return cmd
}

// newOrbInlineCommand returns the "orb inline" cobra.Command.
func newOrbInlineCommand() *cobra.Command {
	var (
		configPath string
		namespace  string
		outputPath string
	)

	cmd := &cobra.Command{
		Use:   "inline",
		Short: "Inline private-orb references into a CircleCI config file.",
		Long: `inline fetches the published YAML source of each orb referenced in the
supplied config file and replaces the external reference with an inline orb
definition, removing the dependency on the orb namespace.

This is intended as a temporary workaround during the migration overlap window
when the source org's private orbs are not yet resolvable in the destination
org.  REVERT the change once the namespace has been transferred.

WARNING: YAML comments and anchors are NOT preserved by the round-trip
serialisation.  For comment-preserving rewrites, apply the change manually.

Example:
  circleci-migrate orb inline \
    --config .circleci/config.yml \
    --namespace myns \
    --output .circleci/config.inlined.yml`,

		RunE: func(cmd *cobra.Command, _ []string) error {
			if configPath == "" {
				return fmt.Errorf("--config is required")
			}

			token := rootOptions.Token
			if token == "" {
				return fmt.Errorf("no API token: set --token or CIRCLECI_CLI_TOKEN")
			}

			data, err := os.ReadFile(configPath)
			if err != nil {
				return fmt.Errorf("reading config %s: %w", configPath, err)
			}

			result, inlined, err := inlineOrbs(rootOptions.Host, token, data, namespace)
			if err != nil {
				return err
			}

			// Determine output writer.
			var out io.Writer
			if outputPath == "" || outputPath == "-" {
				out = cmd.OutOrStdout()
			} else {
				f, err := os.Create(outputPath)
				if err != nil {
					return fmt.Errorf("creating output file %s: %w", outputPath, err)
				}
				defer f.Close() //nolint:errcheck
				out = f
			}

			if _, err := fmt.Fprint(out, result); err != nil {
				return fmt.Errorf("writing output: %w", err)
			}

			// Print summary to stderr.
			errOut := cmd.ErrOrStderr()
			if len(inlined) == 0 {
				fmt.Fprintln(errOut, "No orbs were inlined (nothing matched the filter).")
			} else {
				fmt.Fprintln(errOut, "Inlined orbs:")
				for _, entry := range inlined {
					fmt.Fprintf(errOut, "  - %s  (%s)\n", entry.alias, entry.ref)
				}
			}
			fmt.Fprintln(errOut)
			fmt.Fprintln(errOut, "WARNING: YAML comments and anchors are NOT preserved by this round-trip.")
			fmt.Fprintln(errOut, "REVERT this change after the orb namespace has been transferred to the destination org.")

			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&configPath, "config", "", "Path to the .circleci/config.yml file to rewrite (required)")
	f.StringVar(&namespace, "namespace", "", "Only inline orbs whose namespace matches this value (empty = inline all)")
	f.StringVarP(&outputPath, "output", "o", "", "Output path (default: stdout)")

	return cmd
}

// inlinedEntry records a single orb that was successfully inlined.
type inlinedEntry struct {
	alias string
	ref   string
}

// inlineOrbs parses configYAML, replaces matching scalar orb references with
// inline orb mappings, re-serialises the document, and returns the resulting
// YAML string together with a list of orbs that were inlined.
//
// namespace filters which orbs are inlined: if empty, all scalar orb references
// are candidates; if non-empty, only those whose namespace part equals namespace.
func inlineOrbs(host, token string, configYAML []byte, namespace string) (string, []inlinedEntry, error) {
	// Parse the full document as a yaml.Node tree so we can perform targeted
	// node-level surgery while keeping as much of the original structure as
	// possible (though comments and anchors are still lost on round-trip).
	var doc yaml.Node
	if err := yaml.Unmarshal(configYAML, &doc); err != nil {
		return "", nil, fmt.Errorf("parsing config YAML: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return "", nil, fmt.Errorf("unexpected YAML document structure")
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return "", nil, fmt.Errorf("config root is not a YAML mapping")
	}

	// Locate the top-level "orbs:" mapping.
	orbsNode := findMappingValue(root, "orbs")
	if orbsNode == nil {
		// No orbs section — nothing to do.
		out, err := marshalDocument(&doc)
		return out, nil, err
	}
	if orbsNode.Kind != yaml.MappingNode {
		return "", nil, fmt.Errorf("top-level 'orbs' is not a YAML mapping")
	}

	var inlined []inlinedEntry

	// orbsNode.Content is a flat [key, value, key, value, …] slice.
	for i := 0; i+1 < len(orbsNode.Content); i += 2 {
		keyNode := orbsNode.Content[i]
		valNode := orbsNode.Content[i+1]

		// Skip entries that are already inline (value is a mapping, not a scalar).
		if valNode.Kind != yaml.ScalarNode {
			continue
		}

		ref := valNode.Value // e.g. "myns/myorb@1.2.3"
		alias := keyNode.Value

		// Apply namespace filter.
		if namespace != "" && !refMatchesNamespace(ref, namespace) {
			continue
		}

		// Fetch the orb's YAML source.
		src, err := fetchOrbSource(host, token, ref)
		if err != nil {
			return "", nil, fmt.Errorf("fetching orb %q (%s): %w", alias, ref, err)
		}

		// Build the inline orb node from the fetched source.
		inlineNode, err := buildInlineOrbNode(src)
		if err != nil {
			return "", nil, fmt.Errorf("building inline node for orb %q: %w", alias, err)
		}

		// Replace the scalar value node with the inline mapping node in-place.
		orbsNode.Content[i+1] = inlineNode

		inlined = append(inlined, inlinedEntry{alias: alias, ref: ref})
	}

	out, err := marshalDocument(&doc)
	return out, inlined, err
}

// refMatchesNamespace returns true when the orb reference's namespace segment
// equals ns.  The reference format is "namespace/orb@version" or
// "namespace/orb" (no version).
func refMatchesNamespace(ref, ns string) bool {
	slash := strings.Index(ref, "/")
	if slash < 0 {
		return false
	}
	return ref[:slash] == ns
}

// findMappingValue searches a YAML MappingNode for a key equal to name and
// returns the corresponding value node, or nil if not found.
func findMappingValue(mapping *yaml.Node, name string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == name {
			return mapping.Content[i+1]
		}
	}
	return nil
}

// buildInlineOrbNode parses orbSource YAML, strips disallowed top-level keys
// (version, description, display, orbs), and returns the resulting MappingNode
// ready to be embedded as an inline orb value.
func buildInlineOrbNode(orbSource string) (*yaml.Node, error) {
	var srcDoc yaml.Node
	if err := yaml.Unmarshal([]byte(orbSource), &srcDoc); err != nil {
		return nil, fmt.Errorf("parsing orb source YAML: %w", err)
	}
	if srcDoc.Kind != yaml.DocumentNode || len(srcDoc.Content) == 0 {
		return nil, fmt.Errorf("unexpected orb source YAML structure")
	}
	srcRoot := srcDoc.Content[0]
	if srcRoot.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("orb source root is not a YAML mapping")
	}

	// Build a new MappingNode that contains only the allowed keys.
	inline := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for i := 0; i+1 < len(srcRoot.Content); i += 2 {
		key := srcRoot.Content[i].Value
		if orbKeysToKeep[key] {
			inline.Content = append(inline.Content, srcRoot.Content[i], srcRoot.Content[i+1])
		}
	}

	return inline, nil
}

// marshalDocument serialises a *yaml.Node (expected to be a DocumentNode)
// back to a YAML string with a trailing newline.
func marshalDocument(doc *yaml.Node) (string, error) {
	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return "", fmt.Errorf("serialising YAML: %w", err)
	}
	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("closing YAML encoder: %w", err)
	}
	return buf.String(), nil
}
