package favorites

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPublishDirectoryNoReplaceKeepsExistingDestination(t *testing.T) {
	parent := t.TempDir()
	staging := filepath.Join(parent, ".sticker-export-staging")
	if err := os.Mkdir(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "manifest.json"), []byte("staged"), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(parent, "destination")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "keep.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := publishDirectoryNoReplace(staging, destination)
	if !errors.Is(err, errExportDestinationExists) {
		t.Fatalf("publish error = %v, want destination exists", err)
	}
	if _, err := os.Stat(filepath.Join(staging, "manifest.json")); err != nil {
		t.Fatalf("staging was unexpectedly moved: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(destination, "keep.txt")); err != nil || string(got) != "keep" {
		t.Fatalf("existing destination changed: %q %v", got, err)
	}
}
