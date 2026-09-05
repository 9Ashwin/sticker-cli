//go:build !windows

package packs

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/9Ashwin/sticker-cli/internal/library"
)

func TestStagedImageWriteRejectsNestedSymlink(t *testing.T) {
	stageRoot := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(stageRoot, library.EmoticonsDirectory)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	content := []byte("GIF89a secure staging")
	item := makeItem(content, "secure staging")
	err := writeStagedStream(context.Background(), stageRoot, bytes.NewReader(content), item, false)
	assertPackError(t, err, "validation", "unsafe_path")
	if _, statErr := os.Stat(filepath.Join(outside, filepath.Base(filepath.FromSlash(item.Filename)))); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("staging write escaped through nested symlink: %v", statErr)
	}
}
