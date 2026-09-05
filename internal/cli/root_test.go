package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"version"}, &out, &errOut, "v0.1.0", "abc123"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var got struct {
		OK   bool `json:"ok"`
		Data struct {
			Version string `json:"version"`
			Commit  string `json:"commit"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.OK || got.Data.Version != "v0.1.0" || got.Data.Commit != "abc123" || errOut.Len() != 0 {
		t.Fatalf("unexpected streams: %s / %s", out.String(), errOut.String())
	}
}

func TestUnknownCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"missing"}, &out, &errOut, "dev", "unknown"); code != 2 || out.Len() != 0 || !json.Valid(errOut.Bytes()) {
		t.Fatalf("exit %d: %s / %s", code, out.String(), errOut.String())
	}
}

func TestSchemaListsCommands(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"schema"}, &out, &errOut, "dev", "unknown"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Commands []string `json:"commands"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || !contains(envelope.Data.Commands, "help") || !contains(envelope.Data.Commands, "setup") || !contains(envelope.Data.Commands, "packs install") || !contains(envelope.Data.Commands, "favorites import") || !contains(envelope.Data.Commands, "favorites collections create") || !contains(envelope.Data.Commands, "favorites organize") || contains(envelope.Data.Commands, "completion") || contains(envelope.Data.Commands, "packs") || contains(envelope.Data.Commands, "favorites") || contains(envelope.Data.Commands, "favorites collections") {
		t.Fatalf("unexpected command list: %s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
	out.Reset()
	if code := Run(context.Background(), []string{"schema", "favorites", "list"}, &out, &errOut, "dev", "unknown"); code != 0 {
		t.Fatalf("favorites list schema exit %d: %s", code, errOut.String())
	}
	for _, want := range []string{`"name":"--collection"`, `"name":"--sort"`, `"default":"manual"`} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("favorites list schema is missing %s: %s", want, out.String())
		}
	}
}

func TestFavoriteCollectionSchemaIncludesRegisteredFlags(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"schema", "favorites", "organize"}, &out, &errOut, "dev", "unknown"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var envelope struct {
		Data struct {
			Parameters []struct {
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"parameters"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct{ name, typ string }{{"--collection", "string"}, {"--ids", "string[]"}, {"--move-to", "string"}, {"--order", "string[]"}, {"--dry-run", "bool"}} {
		found := false
		for _, parameter := range envelope.Data.Parameters {
			if parameter.Name == want.name && parameter.Type == want.typ {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("schema is missing %s (%s): %s", want.name, want.typ, out.String())
		}
	}
	if errOut.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}

func TestSchemaIncludesActualParameters(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"schema", "packs", "install"}, &out, &errOut, "dev", "unknown"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Command    string `json:"command"`
			Effect     string `json:"effect"`
			Parameters []struct {
				Name     string `json:"name"`
				Required bool   `json:"required"`
			} `json:"parameters"`
			ResultSchema map[string]any `json:"result_schema"`
			Errors       []schemaError  `json:"errors"`
		} `json:"data"`
		Meta map[string]int `json:"meta"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Data.Command != "packs install" || envelope.Data.Effect != effectWrite || envelope.Meta["schema_version"] != 1 {
		t.Fatalf("unexpected schema envelope: %s", out.String())
	}
	if !hasRequiredParameter(envelope.Data.Parameters, "id") || !hasParameter(envelope.Data.Parameters, "--dry-run") || !hasParameter(envelope.Data.Parameters, "--format") {
		t.Fatalf("schema parameters do not match registered flags: %s", out.String())
	}
	if envelope.Data.ResultSchema["type"] != "object" || len(envelope.Data.Errors) == 0 {
		t.Fatalf("schema contract is incomplete: %s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}

func TestGetSchemaDeclaresPreviewFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"schema", "get"}, &out, &errOut, "dev", "unknown"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), `"name":"--preview"`) || !strings.Contains(out.String(), "static WebP") {
		t.Fatalf("get schema is missing preview contract: %s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}

	out.Reset()
	if code := Run(context.Background(), []string{"get", "0123456789abcdef0123456789abcdef", "--help"}, &out, &errOut, "dev", "unknown"); code != 0 {
		t.Fatalf("help exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "--preview") {
		t.Fatalf("get help is missing preview flag: %s", out.String())
	}
}

func TestSetupSchemaDeclaresPackSelection(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"schema", "setup"}, &out, &errOut, "dev", "unknown"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	for _, want := range []string{`"name":"--pack"`, `"default":"curated"`, `"name":"--source"`, `"name":"--dry-run"`} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("setup schema is missing %s: %s", want, out.String())
		}
	}
	if errOut.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}

	out.Reset()
	if code := Run(context.Background(), []string{"setup", "--pack", "all", "--help"}, &out, &errOut, "dev", "unknown"); code != 0 {
		t.Fatalf("help exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "--pack") || !strings.Contains(out.String(), "curated") || !strings.Contains(out.String(), "all") {
		t.Fatalf("setup help is missing pack selection: %s", out.String())
	}
}

func TestHelpAndSchemaUseTheSameCommandContract(t *testing.T) {
	var help, schema bytes.Buffer
	if code := Run(context.Background(), []string{"packs", "install", "--help"}, &help, &bytes.Buffer{}, "dev", "unknown"); code != 0 {
		t.Fatalf("help exit %d", code)
	}
	if code := Run(context.Background(), []string{"schema", "packs", "install"}, &schema, &bytes.Buffer{}, "dev", "unknown"); code != 0 {
		t.Fatalf("schema exit %d", code)
	}
	if !strings.Contains(help.String(), "sticker packs install ID") || !strings.Contains(help.String(), "--dry-run") || !strings.Contains(help.String(), "sticker packs install curated") {
		t.Fatalf("help does not describe the command: %s", help.String())
	}
	var envelope struct {
		Data struct {
			Examples []string `json:"examples"`
		} `json:"data"`
	}
	if err := json.Unmarshal(schema.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data.Examples) == 0 || !strings.Contains(envelope.Data.Examples[0], "sticker packs install") {
		t.Fatalf("schema examples do not match help: %s", schema.String())
	}
}

func TestRootHelpIncludesExamples(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"--help"}, &out, &errOut, "dev", "unknown"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Examples:") || !strings.Contains(out.String(), "sticker schema packs install") {
		t.Fatalf("root help does not include registered examples: %s", out.String())
	}
	if strings.Contains(out.String(), "completion") {
		t.Fatalf("root help unexpectedly exposes automatic completion: %s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}

func TestBareCommandsUseStructuredOutput(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"packs"},
		{"favorites"},
		{"favorites", "collections"},
	} {
		var out, errOut bytes.Buffer
		if code := Run(context.Background(), args, &out, &errOut, "dev", "unknown"); code != 0 {
			t.Fatalf("args %v: exit %d: %s", args, code, errOut.String())
		}
		var envelope struct {
			OK   bool `json:"ok"`
			Data struct {
				Help string `json:"help"`
			} `json:"data"`
		}
		if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
			t.Fatalf("args %v: unstructured output %q: %v", args, out.String(), err)
		}
		if !envelope.OK || !strings.Contains(envelope.Data.Help, "Usage:") || errOut.Len() != 0 {
			t.Fatalf("args %v: unexpected streams: %s / %s", args, out.String(), errOut.String())
		}
	}

	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"--format", "table", "packs"}, &out, &errOut, "dev", "unknown"); code != 0 {
		t.Fatalf("table group exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "help\t") || !strings.Contains(out.String(), "Usage:") || errOut.Len() != 0 {
		t.Fatalf("unexpected table group output: %s / %s", out.String(), errOut.String())
	}
}

func TestHelpCommandUsesStructuredOutputAndRejectsUnknownTopics(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"help", "packs", "install"}, &out, &errOut, "dev", "unknown"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Help string `json:"help"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || !strings.Contains(envelope.Data.Help, "Usage:") || !strings.Contains(envelope.Data.Help, "sticker packs install ID") || errOut.Len() != 0 {
		t.Fatalf("unexpected structured help: %s / %s", out.String(), errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Run(context.Background(), []string{"--format", "table", "help", "packs", "install"}, &out, &errOut, "dev", "unknown"); code != 0 {
		t.Fatalf("table help exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "help\t") || !strings.Contains(out.String(), "Usage:") || errOut.Len() != 0 {
		t.Fatalf("unexpected table help: %s / %s", out.String(), errOut.String())
	}

	for _, topic := range []string{"missing", "completion", "__complete"} {
		out.Reset()
		errOut.Reset()
		if code := Run(context.Background(), []string{"help", topic}, &out, &errOut, "dev", "unknown"); code != 2 || out.Len() != 0 {
			t.Fatalf("unknown help topic %q: exit %d stdout %q stderr %q", topic, code, out.String(), errOut.String())
		}
		assertError(t, errOut.Bytes(), "validation", "invalid_argument")
	}
}

func TestHiddenCompletionEndpointsAreRejected(t *testing.T) {
	for _, endpoint := range []string{"__complete", "__completeNoDesc"} {
		var out, errOut bytes.Buffer
		if code := Run(context.Background(), []string{endpoint, ""}, &out, &errOut, "dev", "unknown"); code != 2 || out.Len() != 0 {
			t.Fatalf("endpoint %q: exit %d stdout %q stderr %q", endpoint, code, out.String(), errOut.String())
		}
		assertError(t, errOut.Bytes(), "validation", "invalid_argument")
	}
}

func TestHelpDoesNotBypassArgumentValidation(t *testing.T) {
	for _, args := range [][]string{
		{"packs", "nope", "--help"},
		{"packs", "install", "--help", "extra"},
		{"--format", "xml", "--help"},
		{"--format", "table", "--json", "--help"},
	} {
		var out, errOut bytes.Buffer
		if code := Run(context.Background(), args, &out, &errOut, "dev", "unknown"); code != 2 || out.Len() != 0 {
			t.Fatalf("args %v: exit %d stdout %q stderr %q", args, code, out.String(), errOut.String())
		}
		assertError(t, errOut.Bytes(), "validation", "invalid_argument")
	}
}

func TestExplicitBooleanValuesRemainValidAroundHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"--help=false", "version"}, &out, &errOut, "v1", "abc"); code != 0 {
		t.Fatalf("--help=false version exit %d: %s", code, errOut.String())
	}
	if !json.Valid(out.Bytes()) || errOut.Len() != 0 {
		t.Fatalf("unexpected --help=false streams: %s / %s", out.String(), errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Run(context.Background(), []string{"--format", "table", "--json=true", "--help"}, &out, &errOut, "v1", "abc"); code != 2 || out.Len() != 0 {
		t.Fatalf("expected --json=true conflict, got exit %d stdout %q stderr %q", code, out.String(), errOut.String())
	}
	assertError(t, errOut.Bytes(), "validation", "invalid_argument")

	out.Reset()
	errOut.Reset()
	if code := Run(context.Background(), []string{"--format", "table", "--json=false", "--help"}, &out, &errOut, "v1", "abc"); code != 0 || !strings.Contains(out.String(), "Usage:") || errOut.Len() != 0 {
		t.Fatalf("unexpected --json=false help streams: %d %q / %q", code, out.String(), errOut.String())
	}
}

func TestGetSchemaDeclaresPreviewPath(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"schema", "get"}, &out, &errOut, "dev", "unknown"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var envelope struct {
		Data struct {
			ResultSchema struct {
				Properties struct {
					Item struct {
						Properties map[string]struct {
							Type string `json:"type"`
						} `json:"properties"`
					} `json:"item"`
				} `json:"properties"`
			} `json:"result_schema"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if got := envelope.Data.ResultSchema.Properties.Item.Properties["preview_path"].Type; got != "string" {
		t.Fatalf("preview_path schema type = %q, want string: %s", got, out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}

func TestFavoriteRemoveAcceptsMultipleIDs(t *testing.T) {
	home := t.TempDir()
	var out, errOut bytes.Buffer
	ids := []string{"0123456789abcdef0123456789abcdef", "abcdef0123456789abcdef0123456789"}
	if code := Run(context.Background(), append([]string{"--home", home, "favorites", "remove"}, ids...), &out, &errOut, "dev", "unknown"); code != 0 || !json.Valid(out.Bytes()) || errOut.Len() != 0 {
		t.Fatalf("unexpected remove result: exit %d stdout %q stderr %q", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), `"removed":0`) || !strings.Contains(out.String(), `"committed":false`) {
		t.Fatalf("unexpected no-op remove result: %s", out.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Run(context.Background(), []string{"schema", "favorites", "remove"}, &out, &errOut, "dev", "unknown"); code != 0 {
		t.Fatalf("schema exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), `"name":"ids"`) || !strings.Contains(out.String(), `"type":"array"`) {
		t.Fatalf("remove schema is missing ID list: %s", out.String())
	}
}

func TestOutputFormatsAndJSONAlias(t *testing.T) {
	tests := []struct {
		name string
		args []string
		json bool
	}{
		{name: "default", args: []string{"version"}, json: true},
		{name: "alias", args: []string{"--json", "version"}, json: true},
		{name: "table", args: []string{"--format", "table", "version"}, json: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			if code := Run(context.Background(), test.args, &out, &errOut, "v1", "abc"); code != 0 {
				t.Fatalf("exit %d: %s", code, errOut.String())
			}
			if test.json != json.Valid(out.Bytes()) {
				t.Fatalf("unexpected output format: %s", out.String())
			}
			if errOut.Len() != 0 {
				t.Fatalf("unexpected stderr: %s", errOut.String())
			}
		})
	}

	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"--format", "table", "--json", "version"}, &out, &errOut, "v1", "abc"); code != 2 || out.Len() != 0 {
		t.Fatalf("expected format conflict, got exit %d stdout %q stderr %q", code, out.String(), errOut.String())
	}
	assertError(t, errOut.Bytes(), "validation", "invalid_argument")
}

func TestValidationErrorsHaveStableStreams(t *testing.T) {
	for _, args := range [][]string{
		{"packs", "install", "--unknown"},
		{"packs", "install"},
		{"favorites", "add"},
		{"favorites", "add", "/tmp/a.gif", "--id", "0123456789abcdef0123456789abcdef"},
		{"packs", "--format", "xml"},
		{"favorites", "--format", "table", "--json"},
		{"search", "query", "--limit", "0"},
		{"search", "query", "--limit", "101"},
		{"search", "query", "--offset", "-1"},
		{"search", "query", "--pack", "Curated"},
		{"get", "bad"},
		{"packs", "install", "Bad"},
		{"favorites", "remove", "bad"},
		{"favorites", "list", "--sort", "recent"},
		{"favorites", "list", "--collection", "Bad"},
		{"favorites", "collections", "create", ""},
		{"favorites", "organize", "--collection", "work"},
	} {
		var out, errOut bytes.Buffer
		if code := Run(context.Background(), args, &out, &errOut, "dev", "unknown"); code != 2 || out.Len() != 0 {
			t.Fatalf("args %v: exit %d stdout %q stderr %q", args, code, out.String(), errOut.String())
		}
		assertError(t, errOut.Bytes(), "validation", "invalid_argument")
	}
}

func TestOversizedErrorUsesInternalExitCode(t *testing.T) {
	var out bytes.Buffer
	code := writeError(&out, &cliError{
		Type:     "validation",
		Subtype:  "invalid_argument",
		Message:  strings.Repeat("x", maxOutputBytes),
		ExitCode: 2,
	})
	if code != 1 || out.Len() > maxOutputBytes {
		t.Fatalf("unexpected oversized error result: exit %d, bytes %d", code, out.Len())
	}
	assertError(t, out.Bytes(), "internal", "unexpected")
}

func TestOutputLimit(t *testing.T) {
	var out bytes.Buffer
	err := writeJSON(&out, map[string]string{"value": strings.Repeat("x", maxOutputBytes)})
	if err == nil || out.Len() != 0 {
		t.Fatalf("expected bounded output error, got %v and %d bytes", err, out.Len())
	}
	var coded *cliError
	if !errors.As(err, &coded) || coded.Type != "validation" || coded.Subtype != "output_limit" || coded.ExitCode != 2 {
		t.Fatalf("unexpected output limit error: %#v", err)
	}
}

func assertError(t *testing.T, encoded []byte, typ, subtype string) {
	t.Helper()
	var envelope struct {
		OK    bool `json:"ok"`
		Error struct {
			Type    string `json:"type"`
			Subtype string `json:"subtype"`
		} `json:"error"`
	}
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		t.Fatalf("invalid error output: %v", err)
	}
	if envelope.OK || envelope.Error.Type != typ || envelope.Error.Subtype != subtype {
		t.Fatalf("unexpected error: %s", encoded)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func hasParameter(values []struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
}, want string) bool {
	for _, value := range values {
		if value.Name == want {
			return true
		}
	}
	return false
}

func hasRequiredParameter(values []struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
}, want string) bool {
	for _, value := range values {
		if value.Name == want && value.Required {
			return true
		}
	}
	return false
}
