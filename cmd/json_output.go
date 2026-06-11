package cmd

import (
	"encoding/json"
	"fmt"
	"io"
)

// marshalJSON encodes v as indented JSON and writes it to w, followed by a
// newline.  It is the shared helper used by every --json output path.
func marshalJSON(w io.Writer, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling JSON output: %w", err)
	}
	_, err = fmt.Fprintf(w, "%s\n", data)
	return err
}
