//go:build !windows

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteRejectsRootSymlinkSwap(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "library")
	outside := filepath.Join(parent, "outside")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	library, _ := New(root)
	backup := filepath.Join(parent, "library-old")
	library.Hooks.BeforeManifest = func() error {
		if err := os.Rename(root, backup); err != nil {
			return err
		}
		return os.Symlink(outside, root)
	}
	err := library.WriteManifest(context.Background(), Manifest{SchemaVersion: 1, Collection: "personal", Items: []Item{}})
	var coded *Error
	if !errors.As(err, &coded) || coded.Kind != "validation" || coded.Subtype != "unsafe_path" {
		t.Fatalf("root symlink swap was not rejected: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(outside, ManifestName)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("write escaped into replacement root: %v", statErr)
	}
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backup, root); err != nil {
		t.Fatal(err)
	}
}
