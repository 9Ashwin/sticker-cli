package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFavoriteAddCLI(t *testing.T) {
	parent := t.TempDir()
	home := filepath.Join(parent, "home")
	source := filepath.Join(parent, "source.gif")
	if err := os.WriteFile(source, []byte("GIF89a cli favorite"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"--home", home, "favorites", "add", source, "--caption", "调皮"}, &out, &errOut, "dev", "test"); code != 0 {
		t.Fatalf("add exit %d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	var response struct {
		OK   bool `json:"ok"`
		Data struct {
			Item struct {
				Caption string   `json:"caption"`
				Path    string   `json:"path"`
				Packs   []string `json:"packs"`
			} `json:"item"`
			Added   bool `json:"added"`
			Updated bool `json:"updated"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.OK || !response.Data.Added || response.Data.Updated || response.Data.Item.Caption != "调皮" || !filepath.IsAbs(response.Data.Item.Path) || response.Data.Item.Packs == nil {
		t.Fatalf("unexpected add response: %s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Run(context.Background(), []string{"--home", home, "favorites", "add", source, "--caption", ""}, &out, &errOut, "dev", "test"); code != 0 {
		t.Fatalf("clear exit %d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	if !bytes.Contains(out.Bytes(), []byte(`"updated":true`)) {
		t.Fatalf("explicit empty caption was not treated as an update: %s", out.String())
	}
}

func TestFavoriteAddCLIDryRunDoesNotWrite(t *testing.T) {
	parent := t.TempDir()
	home := filepath.Join(parent, "home")
	source := filepath.Join(parent, "source.gif")
	if err := os.WriteFile(source, []byte("GIF89a cli dry run"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"--home", home, "favorites", "add", source, "--dry-run"}, &out, &errOut, "dev", "test"); code != 0 {
		t.Fatalf("dry-run exit %d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	if !bytes.Contains(out.Bytes(), []byte(`"dry_run":true`)) || !bytes.Contains(out.Bytes(), []byte(`"added":true`)) {
		t.Fatalf("unexpected dry-run result: %s", out.String())
	}
	if _, err := os.Stat(home); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run created home: %v", err)
	}
}

func TestFavoriteAddSchemaDeclaresDryRun(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"schema", "favorites", "add"}, &out, &errOut, "dev", "test"); code != 0 {
		t.Fatalf("schema exit %d: %s", code, errOut.String())
	}
	if !bytes.Contains(out.Bytes(), []byte(`"name":"--id"`)) || !bytes.Contains(out.Bytes(), []byte(`"name":"--caption"`)) || !bytes.Contains(out.Bytes(), []byte(`"name":"--dry-run"`)) || !bytes.Contains(out.Bytes(), []byte(`"dry_run"`)) {
		t.Fatalf("favorite add schema is incomplete: %s", out.String())
	}
}
