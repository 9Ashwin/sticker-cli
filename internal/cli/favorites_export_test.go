package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFavoriteExportCLI(t *testing.T) {
	parent := t.TempDir()
	home := filepath.Join(parent, "home")
	source := filepath.Join(parent, "source.gif")
	if err := os.WriteFile(source, []byte("GIF89a cli export"), 0o600); err != nil {
		t.Fatal(err)
	}
	var addOut, addErr bytes.Buffer
	if code := Run(context.Background(), []string{"--home", home, "favorites", "add", source, "--caption", "cli export"}, &addOut, &addErr, "dev", "test"); code != 0 {
		t.Fatalf("add exit %d stdout=%q stderr=%q", code, addOut.String(), addErr.String())
	}

	destination := filepath.Join(parent, "export")
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"--home", home, "favorites", "export", destination}, &out, &errOut, "dev", "test"); code != 0 {
		t.Fatalf("export exit %d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	var response struct {
		OK   bool `json:"ok"`
		Data struct {
			Path  string `json:"path"`
			Count int    `json:"count"`
			Size  int64  `json:"size"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.OK || response.Data.Path != destination || response.Data.Count != 1 || response.Data.Size <= 0 || errOut.Len() != 0 {
		t.Fatalf("unexpected export response: stdout=%s stderr=%s", out.String(), errOut.String())
	}

	out.Reset()
	errOut.Reset()
	dryDestination := filepath.Join(parent, "dry-run")
	if code := Run(context.Background(), []string{"--home", home, "favorites", "export", dryDestination, "--dry-run"}, &out, &errOut, "dev", "test"); code != 0 || !bytes.Contains(out.Bytes(), []byte(`"dry_run":true`)) || errOut.Len() != 0 {
		t.Fatalf("unexpected export dry-run: exit %d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	if _, err := os.Stat(dryDestination); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote destination: %v", err)
	}
}

func TestFavoriteExportSchemaDeclaresDryRun(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"schema", "favorites", "export"}, &out, &errOut, "dev", "test"); code != 0 {
		t.Fatalf("schema exit %d: %s", code, errOut.String())
	}
	for _, want := range []string{`"name":"--dry-run"`, `"path"`, `"count"`, `"size"`, `"dry_run"`} {
		if !bytes.Contains(out.Bytes(), []byte(want)) {
			t.Fatalf("favorite export schema is missing %s: %s", want, out.String())
		}
	}
}
