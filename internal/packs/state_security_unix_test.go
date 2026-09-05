//go:build !windows

package packs

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/9Ashwin/sticker-cli/internal/library"
)

func TestReadInstalledStateRejectsSymlinkWithoutReadingOutside(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	state := installedState{
		SchemaVersion: 1,
		ID:            "curated",
		Source:        filepath.Join(outside, "source"),
		Revision:      hashText([]byte(`{"schema_version":1,"collection":"curated","items":[]}`)),
		Manifest:      json.RawMessage(`{"schema_version":1,"collection":"curated","items":[]}`),
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	outsideState := filepath.Join(outside, "curated.json")
	writeFixtureFile(t, outsideState, data)
	stateDir := filepath.Join(home, ".sticker", "packs")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideState, filepath.Join(stateDir, "curated.json")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, installed, err := readInstalledState(home, "curated")
	if installed {
		t.Fatal("symlinked installed state was accepted")
	}
	assertPackError(t, err, "validation", "unsafe_path")
	if _, statErr := os.Stat(filepath.Join(home, library.ManifestName)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("state read created a personal manifest: %v", statErr)
	}
}
