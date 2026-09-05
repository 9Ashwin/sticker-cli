package library

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func testItem(t *testing.T, root string, data []byte, format string) Item {
	t.Helper()
	md5Sum := md5.Sum(data)
	shaSum := sha256.Sum256(data)
	id := hex.EncodeToString(md5Sum[:])
	item := Item{MD5: id, SHA256: hex.EncodeToString(shaSum[:]), Format: format, Size: int64(len(data)), Filename: filepath.ToSlash(filepath.Join(EmoticonsDirectory, id+"."+format)), Caption: "a test image"}
	if err := os.MkdirAll(filepath.Join(root, EmoticonsDirectory), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(item.Filename)), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return item
}

func TestManifestRoundTripSortsItems(t *testing.T) {
	root := t.TempDir()
	first := testItem(t, root, []byte("\x89PNG\r\n\x1a\nfixture"), "png")
	second := testItem(t, root, []byte("GIF89afixture"), "gif")
	library, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{SchemaVersion: 1, Collection: "personal", Items: []Item{second, first}}
	if err := library.WriteManifest(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	loaded, err := library.ReadManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Items) != 2 || loaded.Items[0].MD5 > loaded.Items[1].MD5 {
		t.Fatalf("items were not sorted: %#v", loaded.Items)
	}
	if err := library.Validate(context.Background(), loaded); err != nil {
		t.Fatal(err)
	}
}

func TestExistingCorruptManifestIsNotEmpty(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ManifestName), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	library, _ := New(root)
	_, err := library.ReadManifest(context.Background())
	var coded *Error
	if !errors.As(err, &coded) || coded.Kind != "integrity" || coded.Subtype != "invalid_manifest" {
		t.Fatalf("got %T %v", err, err)
	}
	if err := library.WriteManifest(context.Background(), Manifest{SchemaVersion: 1, Collection: "personal"}); err == nil {
		t.Fatal("corrupt target was silently replaced")
	}
}

func TestManifestRejectsDuplicateKeysAndUnsafeFilename(t *testing.T) {
	root := t.TempDir()
	data := []byte(`{"schema_version":1,"collection":"personal","collection":"other","items":[]}`)
	if err := os.WriteFile(filepath.Join(root, ManifestName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	library, _ := New(root)
	if _, err := library.ReadManifest(context.Background()); err == nil {
		t.Fatal("duplicate keys accepted")
	}
	item := Item{MD5: "0123456789abcdef0123456789abcdef", SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Filename: "../escape.gif", Format: "gif", Size: 1}
	if err := ValidateManifest(Manifest{SchemaVersion: 1, Collection: "personal", Items: []Item{item}}, Limits{}); err == nil {
		t.Fatal("unsafe filename accepted")
	}
}

func TestValidateRejectsHashAndSignatureMismatch(t *testing.T) {
	root := t.TempDir()
	item := testItem(t, root, []byte("GIF89afixture"), "gif")
	item.SHA256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	library, _ := New(root)
	if err := library.Validate(context.Background(), Manifest{SchemaVersion: 1, Collection: "personal", Items: []Item{item}}); err == nil {
		t.Fatal("hash mismatch accepted")
	}
	item = testItem(t, root, []byte("not a gif"), "gif")
	if err := library.Validate(context.Background(), Manifest{SchemaVersion: 1, Collection: "personal", Items: []Item{item}}); err == nil {
		t.Fatal("format signature mismatch accepted")
	}
}

func TestUpdateManifestSerializesConcurrentWriters(t *testing.T) {
	library, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for i := range 8 {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			if err := library.UpdateManifest(context.Background(), func(manifest Manifest) (Manifest, error) {
				manifest.Items = append(manifest.Items, testMetadataItem(i))
				return manifest, nil
			}); err != nil {
				t.Errorf("update %d: %v", i, err)
			}
		}(i)
	}
	group.Wait()
	manifest, err := library.ReadManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Items) != 8 {
		t.Fatalf("concurrent updates lost entries: %d", len(manifest.Items))
	}
}

func testMetadataItem(i int) Item {
	var id [16]byte
	id[15] = byte(i + 1)
	var sha [32]byte
	sha[31] = byte(i + 1)
	return Item{MD5: hex.EncodeToString(id[:]), SHA256: hex.EncodeToString(sha[:]), Filename: filepath.ToSlash(filepath.Join(EmoticonsDirectory, hex.EncodeToString(id[:])+".gif")), Format: "gif", Size: 1}
}

func TestWriteCancellationAndCommittedAfterError(t *testing.T) {
	library, _ := New(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := library.WriteManifest(ctx, Manifest{SchemaVersion: 1, Collection: "personal"}); err == nil {
		t.Fatal("cancelled write succeeded")
	}
	library.Hooks.AfterRename = func(string) error { return errors.New("simulated directory sync failure") }
	err := library.WriteManifest(context.Background(), Manifest{SchemaVersion: 1, Collection: "personal"})
	var coded *Error
	if !errors.As(err, &coded) || !coded.Committed {
		t.Fatalf("post-commit error did not report committed state: %v", err)
	}
	if _, err := library.ReadManifest(context.Background()); err != nil {
		t.Fatalf("committed manifest unreadable: %v", err)
	}
}

func TestLockCancellation(t *testing.T) {
	root := t.TempDir()
	first, err := acquireLock(context.Background(), root, true, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := acquireLock(ctx, root, true, time.Second); err == nil {
		t.Fatal("cancelled lock acquisition succeeded")
	}
}

func TestSymlinkedImageCannotEscapeRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.gif")
	data := []byte("GIF89afixture")
	if err := os.WriteFile(outside, data, 0o600); err != nil {
		t.Fatal(err)
	}
	item := testItem(t, root, data, "gif")
	imagePath := filepath.Join(root, filepath.FromSlash(item.Filename))
	if err := os.Remove(imagePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, imagePath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	library, _ := New(root)
	err := library.Validate(context.Background(), Manifest{SchemaVersion: 1, Collection: "personal", Items: []Item{item}})
	var coded *Error
	if !errors.As(err, &coded) || coded.Kind != "validation" || coded.Subtype != "unsafe_path" {
		t.Fatalf("symlink escape was not rejected: %v", err)
	}
}
