//go:build windows

package packs

import (
	"path/filepath"
	"testing"
)

func TestResolveAcceptsWindowsLocalSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sticker-ext")
	source, err := Resolve(path)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", path, err)
	}
	if !source.IsLocal() || source.LocalRoot != filepath.Clean(path) || source.Canonical != filepath.Clean(path) {
		t.Fatalf("unexpected local source: %+v", source)
	}
}

func TestResolveAcceptsWindowsRelativeSource(t *testing.T) {
	source, err := Resolve(filepath.Join("parent", "sticker-ext"))
	if err != nil {
		t.Fatalf("Resolve relative source: %v", err)
	}
	if !source.IsLocal() {
		t.Fatalf("relative Windows path was not treated as local: %+v", source)
	}
}
