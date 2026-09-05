package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/9Ashwin/sticker-cli/internal/library"
)

func TestSetupDefaultsToCuratedAndDryRunDoesNotCreateHome(t *testing.T) {
	source := t.TempDir()
	item := cliTestItem([]byte("GIF89a setup curated"), "curated setup")
	writeCLIPackVersion(t, source, []library.Item{item}, [][]byte{[]byte("GIF89a setup curated")})
	appendSetupPack(t, source, setupPackDescriptor{
		ID:             "all",
		Name:           "All",
		Description:    "Unused full pack",
		Manifest:       "manifest.json",
		ManifestSHA256: strings.Repeat("0", sha256.Size*2),
	})

	home := filepath.Join(t.TempDir(), "home")
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"--home", home, "setup", "--source", source, "--dry-run"}, &out, &errOut, "dev", "test"); code != 0 {
		t.Fatalf("setup dry-run exit %d: %s", code, errOut.String())
	}
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Setup bool `json:"setup"`
			Pack  struct {
				ID string `json:"id"`
			} `json:"pack"`
			Revision      string `json:"revision"`
			Added         int    `json:"added"`
			Reused        int    `json:"reused"`
			DownloadBytes int64  `json:"download_bytes"`
			DryRun        bool   `json:"dry_run"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || !envelope.Data.Setup || envelope.Data.Pack.ID != "curated" || envelope.Data.Revision == "" || envelope.Data.Added != 1 || envelope.Data.Reused != 0 || envelope.Data.DownloadBytes != item.Size || !envelope.Data.DryRun {
		t.Fatalf("unexpected setup plan: %s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
	if _, err := os.Stat(home); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("setup dry-run created the home directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(source, "manifest.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("curated setup unexpectedly required the full manifest: %v", err)
	}

	out.Reset()
	errOut.Reset()
	if code := Run(context.Background(), []string{"--home", home, "setup", "--source", source}, &out, &errOut, "dev", "test"); code != 0 {
		t.Fatalf("setup exit %d: %s", code, errOut.String())
	}
	var installed struct {
		OK   bool `json:"ok"`
		Data struct {
			Setup         bool  `json:"setup"`
			Added         int   `json:"added"`
			Reused        int   `json:"reused"`
			DownloadBytes int64 `json:"download_bytes"`
			DryRun        bool  `json:"dry_run"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &installed); err != nil {
		t.Fatal(err)
	}
	if !installed.OK || !installed.Data.Setup || installed.Data.Added != 1 || installed.Data.Reused != 0 || installed.Data.DownloadBytes != item.Size || installed.Data.DryRun {
		t.Fatalf("unexpected setup output: %s", out.String())
	}
	if _, err := os.Stat(filepath.Join(home, filepath.FromSlash(item.Filename))); err != nil {
		t.Fatalf("setup did not publish the curated image: %v", err)
	}
}

func TestSetupAllRequiresExplicitSelection(t *testing.T) {
	source := t.TempDir()
	curatedContent := []byte("GIF89a setup curated")
	curated := cliTestItem(curatedContent, "curated setup")
	writeCLIPackVersion(t, source, []library.Item{curated}, [][]byte{curatedContent})

	allContent := []byte("GIF89a setup all-only")
	allOnly := cliTestItem(allContent, "all setup")
	allManifest, err := json.Marshal(library.Manifest{SchemaVersion: 1, Collection: "all", Items: []library.Item{curated, allOnly}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "manifest.json"), allManifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, filepath.FromSlash(allOnly.Filename)), allContent, 0o600); err != nil {
		t.Fatal(err)
	}
	appendSetupPack(t, source, setupPackDescriptor{
		ID:             "all",
		Name:           "All",
		Description:    "Full pack",
		Manifest:       "manifest.json",
		ManifestSHA256: hashBytes(allManifest),
		Count:          2,
		Size:           curated.Size + allOnly.Size,
	})

	home := filepath.Join(t.TempDir(), "home")
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"--home", home, "setup", "--source", source, "--pack", "all"}, &out, &errOut, "dev", "test"); code != 0 {
		t.Fatalf("setup all exit %d: %s", code, errOut.String())
	}
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Setup bool `json:"setup"`
			Pack  struct {
				ID string `json:"id"`
			} `json:"pack"`
			Added         int   `json:"added"`
			DownloadBytes int64 `json:"download_bytes"`
			DryRun        bool  `json:"dry_run"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || !envelope.Data.Setup || envelope.Data.Pack.ID != "all" || envelope.Data.Added != 2 || envelope.Data.DownloadBytes != curated.Size+allOnly.Size || envelope.Data.DryRun {
		t.Fatalf("unexpected all setup output: %s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
	for _, item := range []library.Item{curated, allOnly} {
		if _, err := os.Stat(filepath.Join(home, filepath.FromSlash(item.Filename))); err != nil {
			t.Fatalf("setup all did not publish %s: %v", item.MD5, err)
		}
	}
}

type setupPackDescriptor struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	Manifest       string `json:"manifest"`
	ManifestSHA256 string `json:"manifest_sha256"`
	Count          int    `json:"count"`
	Size           int64  `json:"size"`
}

func appendSetupPack(t *testing.T, source string, pack setupPackDescriptor) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(source, "packs.json"))
	if err != nil {
		t.Fatal(err)
	}
	var directory struct {
		SchemaVersion int                   `json:"schema_version"`
		Packs         []setupPackDescriptor `json:"packs"`
	}
	if err := json.Unmarshal(data, &directory); err != nil {
		t.Fatal(err)
	}
	directory.Packs = append(directory.Packs, pack)
	data, err = json.Marshal(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "packs.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}
