package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

const (
	formatJSON  = "json"
	formatTable = "table"
)

type successEnvelope struct {
	OK   bool           `json:"ok"`
	Data any            `json:"data"`
	Meta map[string]int `json:"meta"`
}

func writeResult(cmd *cobra.Command, options *rootOptions, data any, forceJSON bool) error {
	format := options.format
	if forceJSON {
		format = formatJSON
	}
	envelope := successEnvelope{OK: true, Data: data, Meta: map[string]int{"schema_version": 1}}
	if format == formatTable {
		return writeTable(cmd.OutOrStdout(), data)
	}
	return writeJSON(cmd.OutOrStdout(), envelope)
}

func writeJSON(w io.Writer, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return &cliError{Type: "internal", Subtype: "unexpected", Message: "failed to encode output", ExitCode: 1}
	}
	if len(encoded)+1 > maxOutputBytes {
		return validationError("output_limit", "output exceeds 256 KiB", "Reduce the requested page size or use a narrower query.")
	}
	_, err = fmt.Fprintf(w, "%s\n", encoded)
	if err != nil {
		return &cliError{Type: "io", Subtype: "write_failed", Message: "failed to write output", Hint: err.Error(), ExitCode: 7}
	}
	return nil
}

func writeTable(w io.Writer, data any) error {
	encoded, err := json.Marshal(data)
	if err != nil {
		return &cliError{Type: "internal", Subtype: "unexpected", Message: "failed to encode table output", ExitCode: 1}
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil || object == nil {
		object = map[string]any{"value": string(encoded)}
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	for _, key := range keys {
		value, err := json.Marshal(object[key])
		if err != nil {
			return &cliError{Type: "internal", Subtype: "unexpected", Message: "failed to encode table value", ExitCode: 1}
		}
		text := strings.Trim(string(value), `"`)
		_, _ = fmt.Fprintf(&buf, "%s\t%s\n", key, text)
	}
	if buf.Len() > maxOutputBytes {
		return validationError("output_limit", "output exceeds 256 KiB", "Reduce the requested page size or use JSON with a narrower query.")
	}
	if _, err := io.Copy(w, &buf); err != nil {
		return &cliError{Type: "io", Subtype: "write_failed", Message: "failed to write output", Hint: err.Error(), ExitCode: 7}
	}
	return nil
}
