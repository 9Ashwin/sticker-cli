package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFavoriteCollectionsCLI(t *testing.T) {
	parent := t.TempDir()
	home := filepath.Join(parent, "home")
	firstPath := filepath.Join(parent, "first.gif")
	secondPath := filepath.Join(parent, "second.gif")
	if err := os.WriteFile(firstPath, []byte("GIF89a cli collections first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondPath, []byte("GIF89a cli collections second"), 0o600); err != nil {
		t.Fatal(err)
	}
	add := func(path string) string {
		var out, errOut bytes.Buffer
		if code := Run(context.Background(), []string{"--home", home, "favorites", "add", path}, &out, &errOut, "dev", "test"); code != 0 {
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
	firstID := add(firstPath)
	secondID := add(secondPath)

	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"--home", home, "favorites", "collections", "create", "work"}, &out, &errOut, "dev", "test"); code != 0 || !bytes.Contains(out.Bytes(), []byte(`"id":"work"`)) || errOut.Len() != 0 {
		t.Fatalf("create exit %d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}

	out.Reset()
	if code := Run(context.Background(), []string{"--home", home, "favorites", "organize", "--collection", "favorites", "--ids", firstID, "--move-to", "work"}, &out, &errOut, "dev", "test"); code != 0 || !bytes.Contains(out.Bytes(), []byte(`"moved":1`)) {
		t.Fatalf("move exit %d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}

	out.Reset()
	if code := Run(context.Background(), []string{"--home", home, "favorites", "list", "--collection", "work"}, &out, &errOut, "dev", "test"); code != 0 || !bytes.Contains(out.Bytes(), []byte(`"total":1`)) || !bytes.Contains(out.Bytes(), []byte(firstID)) {
		t.Fatalf("collection list exit %d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}

	out.Reset()
	if code := Run(context.Background(), []string{"--home", home, "favorites", "collections", "rename", "work", "工作"}, &out, &errOut, "dev", "test"); code != 0 || !bytes.Contains(out.Bytes(), []byte(`"name":"工作"`)) {
		t.Fatalf("rename exit %d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}

	out.Reset()
	if code := Run(context.Background(), []string{"--home", home, "favorites", "collections", "remove", "work"}, &out, &errOut, "dev", "test"); code != 0 || !bytes.Contains(out.Bytes(), []byte(`"removed":true`)) || !bytes.Contains(out.Bytes(), []byte(`"moved":1`)) {
		t.Fatalf("remove exit %d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}

	out.Reset()
	if code := Run(context.Background(), []string{"--home", home, "favorites", "list", "--collection", "favorites", "--limit", "10"}, &out, &errOut, "dev", "test"); code != 0 || !bytes.Contains(out.Bytes(), []byte(`"total":2`)) || !bytes.Contains(out.Bytes(), []byte(secondID)) {
		t.Fatalf("default list exit %d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
}
