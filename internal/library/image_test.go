package library

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestReadImageRejectsIntermediateSymlink(t *testing.T) {
	parent := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	writeImageFile(t, filepath.Join(outside, "source.gif"), []byte("GIF89a outside"))
	link := filepath.Join(parent, "images")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, _, err := ReadImage(context.Background(), filepath.Join(link, "source.gif"))
	var coded *Error
	if !errors.As(err, &coded) || coded.Kind != "validation" || coded.Subtype != "unsafe_path" {
		t.Fatalf("intermediate symlink was not rejected: %T %v", err, err)
	}
}

func TestReadImageSourceDirectorySwapDoesNotReadOutside(t *testing.T) {
	parent := t.TempDir()
	realDirectory := filepath.Join(parent, "real")
	outsideDirectory := filepath.Join(parent, "outside")
	if err := os.MkdirAll(realDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outsideDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	allowed := []byte("GIF89a allowed")
	outside := []byte("GIF89a outside")
	writeImageFile(t, filepath.Join(realDirectory, "source.gif"), allowed)
	writeImageFile(t, filepath.Join(outsideDirectory, "source.gif"), outside)
	pivot := filepath.Join(parent, "pivot")
	if err := os.Rename(realDirectory, pivot); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(pivot, "source.gif")
	if _, data, err := ReadImage(context.Background(), path); err != nil || !bytes.Equal(data, allowed) {
		t.Fatalf("stable source read failed: %v", err)
	}

	stop := make(chan struct{})
	var group sync.WaitGroup
	group.Add(1)
	go func() {
		defer group.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = os.Rename(pivot, realDirectory)
			_ = os.Symlink(outsideDirectory, pivot)
			_ = os.Remove(pivot)
			_ = os.Rename(realDirectory, pivot)
		}
	}()

	for range 100 {
		_, data, err := ReadImage(context.Background(), path)
		if err == nil && !bytes.Equal(data, allowed) {
			close(stop)
			group.Wait()
			t.Fatalf("source read escaped to outside data: %q", data)
		}
	}
	close(stop)
	group.Wait()
}

func writeImageFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
