//go:build !windows

package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveRelativeDirectoryDoesNotFollowSymlinks(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	keep := filepath.Join(external, "keep.txt")
	if err := os.WriteFile(keep, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(root, ".sticker-export-test")
	if err := os.MkdirAll(filepath.Join(staging, "emoticons"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "emoticons", "image.gif"), []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(staging, "linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staging, "manifest.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	libraryRoot, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := libraryRoot.RemoveRelativeDirectory(context.Background(), filepath.Base(staging)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatalf("staging directory remains: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("external file was removed through a symlink: %v", err)
	}
}
