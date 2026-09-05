//go:build !windows

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveRelativeRejectsSymlinkTarget(t *testing.T) {
	rootPath := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(rootPath, ".sticker", "packs")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(stateDir, "curated.json")
	if err := os.Symlink(outside, target); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	root, err := New(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	removed, err := root.RemoveRelative(context.Background(), ".sticker/packs/curated.json")
	if removed {
		t.Fatal("symlink target was reported as removed")
	}
	var coded *Error
	if !errors.As(err, &coded) || coded.Kind != "validation" || coded.Subtype != "unsafe_path" {
		t.Fatalf("unexpected symlink error: %T %v", err, err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("outside file was changed: %v", err)
	}
}
