package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFavoriteManageCLI(t *testing.T) {
	parent := t.TempDir()
	home := filepath.Join(parent, "home")
	sources := []string{filepath.Join(parent, "first.gif"), filepath.Join(parent, "second.gif")}
	for index, source := range sources {
		if err := os.WriteFile(source, []byte("GIF89a-cli-manage-"+string(rune('a'+index))), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	add := func(source, caption string) string {
		var out, errOut bytes.Buffer
		args := []string{"--home", home, "favorites", "add", source, "--caption", caption}
		if code := Run(context.Background(), args, &out, &errOut, "dev", "test"); code != 0 {
			t.Fatalf("add exit %d stdout=%q stderr=%q", code, out.String(), errOut.String())
		}
		var response struct {
			Data struct {
				Item struct {
					ID string `json:"id"`
				} `json:"item"`
			} `json:"data"`
		}
		if err := json.Unmarshal(out.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		return response.Data.Item.ID
	}
	firstID := add(sources[0], "zebra")
	secondID := add(sources[1], "alpha")

	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"--home", home, "favorites", "list", "--sort", "caption", "--limit", "1"}, &out, &errOut, "dev", "test"); code != 0 {
		t.Fatalf("list exit %d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	var list struct {
		OK   bool `json:"ok"`
		Data struct {
			Items []struct {
				ID      string `json:"id"`
				Caption string `json:"caption"`
			} `json:"items"`
			Total      int  `json:"total"`
			NextOffset int  `json:"next_offset"`
			HasMore    bool `json:"has_more"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if !list.OK || list.Data.Total != 2 || len(list.Data.Items) != 1 || list.Data.Items[0].ID != secondID || list.Data.Items[0].Caption != "alpha" || list.Data.NextOffset != 1 || !list.Data.HasMore || errOut.Len() != 0 {
		t.Fatalf("unexpected list response: %s / %s", out.String(), errOut.String())
	}

	out.Reset()
	if code := Run(context.Background(), []string{"--home", home, "favorites", "describe", firstID, "--caption", "updated", "--dry-run"}, &out, &errOut, "dev", "test"); code != 0 || !bytes.Contains(out.Bytes(), []byte(`"updated":true`)) || !bytes.Contains(out.Bytes(), []byte(`"dry_run":true`)) {
		t.Fatalf("unexpected describe dry-run: exit %d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Run(context.Background(), []string{"--home", home, "favorites", "describe", firstID, "--caption", "updated"}, &out, &errOut, "dev", "test"); code != 0 || !bytes.Contains(out.Bytes(), []byte(`"updated":true`)) || errOut.Len() != 0 {
		t.Fatalf("unexpected describe: exit %d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}

	out.Reset()
	if code := Run(context.Background(), []string{"--home", home, "favorites", "remove", firstID, "--dry-run"}, &out, &errOut, "dev", "test"); code != 0 || !bytes.Contains(out.Bytes(), []byte(`"removed":1`)) || !bytes.Contains(out.Bytes(), []byte(`"committed":false`)) {
		t.Fatalf("unexpected remove dry-run: exit %d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	if _, err := os.Stat(filepath.Join(home, "manifest.json")); err != nil {
		t.Fatalf("remove dry-run changed manifest: %v", err)
	}

	out.Reset()
	errOut.Reset()
	if code := Run(context.Background(), []string{"--home", home, "favorites", "remove", firstID}, &out, &errOut, "dev", "test"); code != 0 || !bytes.Contains(out.Bytes(), []byte(`"removed":1`)) || !bytes.Contains(out.Bytes(), []byte(`"committed":true`)) || errOut.Len() != 0 {
		t.Fatalf("unexpected remove: exit %d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}

	out.Reset()
	if code := Run(context.Background(), []string{"--home", home, "favorites", "remove", firstID}, &out, &errOut, "dev", "test"); code != 0 || !bytes.Contains(out.Bytes(), []byte(`"removed":0`)) || !bytes.Contains(out.Bytes(), []byte(`"committed":false`)) {
		t.Fatalf("unexpected repeated remove: exit %d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
}

func TestFavoriteDescribeRequiresCaptionAndMapsMissingFavorite(t *testing.T) {
	home := t.TempDir()
	id := "0123456789abcdef0123456789abcdef"
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"--home", home, "favorites", "describe", id}, &out, &errOut, "dev", "test"); code != 2 || out.Len() != 0 {
		t.Fatalf("missing caption exit %d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	assertError(t, errOut.Bytes(), "validation", "invalid_argument")

	out.Reset()
	errOut.Reset()
	if code := Run(context.Background(), []string{"--home", home, "favorites", "describe", id, "--caption", "new"}, &out, &errOut, "dev", "test"); code != 3 || out.Len() != 0 {
		t.Fatalf("missing favorite exit %d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	assertError(t, errOut.Bytes(), "not_found", "item_not_found")
}

func TestFavoriteManageSchemaDeclaresDryRun(t *testing.T) {
	for _, command := range [][]string{{"schema", "favorites", "describe"}, {"schema", "favorites", "remove"}} {
		var out, errOut bytes.Buffer
		if code := Run(context.Background(), command, &out, &errOut, "dev", "test"); code != 0 {
			t.Fatalf("%v schema exit %d: %s", command, code, errOut.String())
		}
		if !bytes.Contains(out.Bytes(), []byte(`"name":"--dry-run"`)) || !bytes.Contains(out.Bytes(), []byte(`"dry_run"`)) {
			t.Fatalf("%v schema is missing dry-run contract: %s", command, out.String())
		}
	}
}
