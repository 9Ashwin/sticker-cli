package cli

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/9Ashwin/sticker-cli/internal/library"
)

func TestSearchCommandUsesHomeAndReturnsBoundedJSON(t *testing.T) {
	root := t.TempDir()
	data := []byte("GIF89a-cli-search")
	md5Sum := md5.Sum(data)
	shaSum := sha256.Sum256(data)
	id := hex.EncodeToString(md5Sum[:])
	item := library.Item{
		MD5:      id,
		SHA256:   hex.EncodeToString(shaSum[:]),
		Filename: filepath.ToSlash(filepath.Join(library.EmoticonsDirectory, id+".gif")),
		Format:   "gif",
		Size:     int64(len(data)),
		Caption:  "调皮回应",
	}
	manifest, err := library.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.WriteManifest(context.Background(), library.Manifest{SchemaVersion: 1, Collection: "personal", Items: []library.Item{item}}); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"--home", root, "search", "回应", "--limit", "1"}, &out, &errOut, "dev", "test"); code != 0 {
		t.Fatalf("search exit %d: %s", code, errOut.String())
	}
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Items []struct {
				ID       string `json:"id"`
				Caption  string `json:"caption"`
				Path     string `json:"path"`
				Favorite bool   `json:"favorite"`
			} `json:"items"`
			Total   int  `json:"total"`
			Next    int  `json:"next_offset"`
			HasMore bool `json:"has_more"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Data.Total != 1 || len(envelope.Data.Items) != 1 || envelope.Data.Next != 1 || envelope.Data.HasMore || envelope.Data.Items[0].ID != id || envelope.Data.Items[0].Caption != item.Caption || !envelope.Data.Items[0].Favorite || !filepath.IsAbs(envelope.Data.Items[0].Path) {
		t.Fatalf("unexpected search response: %s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}

func TestSearchCommandMapsMissingPackError(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"--home", t.TempDir(), "search", "anything", "--pack", "missing"}, &out, &errOut, "dev", "test"); code != 3 || out.Len() != 0 {
		t.Fatalf("missing pack exit %d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	var envelope struct {
		OK    bool `json:"ok"`
		Error struct {
			Type    string `json:"type"`
			Subtype string `json:"subtype"`
		} `json:"error"`
	}
	if err := json.Unmarshal(errOut.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.OK || envelope.Error.Type != "not_found" || envelope.Error.Subtype != "pack_not_found" {
		t.Fatalf("unexpected missing pack error: %s", errOut.String())
	}
}
