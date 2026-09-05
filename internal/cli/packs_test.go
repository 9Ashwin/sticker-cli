package cli

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/9Ashwin/sticker-cli/internal/library"
)

func TestPackListReadsExplicitLocalSource(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "packs.json"), []byte(`{"schema_version":1,"packs":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"--home", home, "packs", "list", "--source", source}, &out, &errOut, "dev", "unknown"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Source string `json:"source"`
			Items  []any  `json:"items"`
			Stale  bool   `json:"stale"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Data.Source != source || len(envelope.Data.Items) != 0 || envelope.Data.Stale {
		t.Fatalf("unexpected result: %s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}

func TestPackInstallDryRunReturnsPlanWithoutCreatingHome(t *testing.T) {
	source := t.TempDir()
	if err := os.MkdirAll(filepath.Join(source, "packs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(source, library.EmoticonsDirectory), 0o700); err != nil {
		t.Fatal(err)
	}
	content := []byte("GIF89a cli fixture")
	md5sum := md5.Sum(content)
	shaSum := sha256.Sum256(content)
	md5Text := hex.EncodeToString(md5sum[:])
	manifest := library.Manifest{SchemaVersion: 1, Collection: "curated", Items: []library.Item{{
		MD5: md5Text, SHA256: hex.EncodeToString(shaSum[:]), Filename: "emoticons/" + md5Text + ".gif", Format: "gif", Size: int64(len(content)), Caption: "cli fixture",
	}}}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestSum := sha256.Sum256(manifestBytes)
	directory := struct {
		SchemaVersion int `json:"schema_version"`
		Packs         []struct {
			ID             string `json:"id"`
			Name           string `json:"name"`
			Description    string `json:"description"`
			Manifest       string `json:"manifest"`
			ManifestSHA256 string `json:"manifest_sha256"`
			Count          int    `json:"count"`
			Size           int64  `json:"size"`
		} `json:"packs"`
	}{SchemaVersion: 1}
	directory.Packs = append(directory.Packs, struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		Description    string `json:"description"`
		Manifest       string `json:"manifest"`
		ManifestSHA256 string `json:"manifest_sha256"`
		Count          int    `json:"count"`
		Size           int64  `json:"size"`
	}{"curated", "Curated", "A small selection", "packs/curated.json", hex.EncodeToString(manifestSum[:]), 1, int64(len(content))})
	directoryBytes, err := json.Marshal(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "packs.json"), directoryBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "packs", "curated.json"), manifestBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, filepath.FromSlash(manifest.Items[0].Filename)), content, 0o600); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(t.TempDir(), "home")
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"--home", home, "packs", "install", "curated", "--source", source, "--dry-run"}, &out, &errOut, "dev", "unknown"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Target        string `json:"target"`
			Added         int    `json:"added"`
			Reused        int    `json:"reused"`
			DownloadBytes int64  `json:"download_bytes"`
			DryRun        bool   `json:"dry_run"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Data.Target != home || envelope.Data.Added != 1 || envelope.Data.Reused != 0 || envelope.Data.DownloadBytes != int64(len(content)) || !envelope.Data.DryRun {
		t.Fatalf("unexpected plan output: %s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("dry-run created the home directory: %v", err)
	}
	out.Reset()
	errOut.Reset()
	if code := Run(context.Background(), []string{"--home", home, "packs", "install", "curated", "--source", source}, &out, &errOut, "dev", "unknown"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var installed struct {
		OK   bool `json:"ok"`
		Data struct {
			Added         int   `json:"added"`
			Reused        int   `json:"reused"`
			DownloadBytes int64 `json:"download_bytes"`
			DryRun        bool  `json:"dry_run"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &installed); err != nil {
		t.Fatal(err)
	}
	if !installed.OK || installed.Data.Added != 1 || installed.Data.Reused != 0 || installed.Data.DownloadBytes != int64(len(content)) || installed.Data.DryRun {
		t.Fatalf("unexpected install output: %s", out.String())
	}
}
