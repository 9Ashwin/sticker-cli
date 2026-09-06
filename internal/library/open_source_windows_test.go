//go:build windows

package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSameWindowsPathAcceptsFilesystemAliases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Image.GIF")
	if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := strings.ToLower(filepath.ToSlash(path))
	if !sameWindowsPath(path, alias) {
		t.Fatalf("Windows path aliases should identify the same file: %q and %q", path, alias)
	}
}
