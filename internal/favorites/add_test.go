package favorites

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/9Ashwin/sticker-cli/internal/library"
)

func TestExecutePathCopiesOriginalAndSurvivesSourceMove(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "source.gif")
	data := []byte("GIF89a favorite source")
	writeFile(t, source, data)
	home := filepath.Join(parent, "home")

	result, err := Execute(context.Background(), Options{Home: home, Path: source})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Added || result.Updated || result.DryRun || result.Item.Caption != "" {
		t.Fatalf("unexpected add result: %+v", result)
	}
	if !filepath.IsAbs(result.Item.Path) {
		t.Fatalf("result path is not absolute: %q", result.Item.Path)
	}
	if got, err := os.ReadFile(result.Item.Path); err != nil || string(got) != string(data) {
		t.Fatalf("copied image mismatch: %q %v", got, err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	root, err := library.New(home)
	if err != nil {
		t.Fatal(err)
	}
	item, path, err := root.ReadItem(context.Background(), result.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if item.MD5 != result.Item.ID || path != result.Item.Path {
		t.Fatalf("moved source changed personal item: item=%+v path=%q", item, path)
	}
}

func TestExecuteCaptionSemanticsAndDeduplication(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "source.gif")
	writeFile(t, source, []byte("GIF89a caption source"))
	home := filepath.Join(parent, "home")

	first, err := Execute(context.Background(), Options{Home: home, Path: source})
	if err != nil || !first.Added {
		t.Fatalf("first add = %+v, %v", first, err)
	}
	second, err := Execute(context.Background(), Options{Home: home, Path: source})
	if err != nil || second.Added || second.Updated {
		t.Fatalf("duplicate add = %+v, %v", second, err)
	}
	caption := "调皮场景"
	third, err := Execute(context.Background(), Options{Home: home, Path: source, Caption: &caption})
	if err != nil || third.Added || !third.Updated || third.Item.Caption != caption {
		t.Fatalf("caption update = %+v, %v", third, err)
	}
	fourth, err := Execute(context.Background(), Options{Home: home, Path: source})
	if err != nil || fourth.Item.Caption != caption || fourth.Updated {
		t.Fatalf("implicit caption preservation = %+v, %v", fourth, err)
	}
	cleared := ""
	fifth, err := Execute(context.Background(), Options{Home: home, Path: source, Caption: &cleared})
	if err != nil || fifth.Added || !fifth.Updated || fifth.Item.Caption != "" {
		t.Fatalf("explicit caption clear = %+v, %v", fifth, err)
	}

	root, err := library.New(home)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := root.ReadManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Items) != 1 || manifest.Items[0].Caption != "" {
		t.Fatalf("deduplication or caption clear failed: %+v", manifest)
	}
}

func TestExecuteIDInheritsInstalledCaption(t *testing.T) {
	parent := t.TempDir()
	home := filepath.Join(parent, "home")
	data := []byte("GIF89a installed source")
	item := itemForData(data, "installed caption")
	writeFile(t, filepath.Join(home, filepath.FromSlash(item.Filename)), data)
	state := map[string]any{
		"schema_version": 1,
		"id":             "curated",
		"source":         "local",
		"revision":       strings.Repeat("a", 64),
		"installed_at":   "2026-09-06T00:00:00Z",
		"manifest": library.Manifest{
			SchemaVersion: 1,
			Collection:    "curated",
			Items:         []library.Item{item},
		},
	}
	stateBytes, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(home, ".sticker", "packs", "curated.json"), stateBytes)

	result, err := Execute(context.Background(), Options{Home: home, ID: item.MD5})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Added || result.Item.Caption != item.Caption || len(result.Item.Packs) != 1 || result.Item.Packs[0] != "curated" {
		t.Fatalf("installed ID was not added with source metadata: %+v", result)
	}
	root, err := library.New(home)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := root.ReadManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Items) != 1 || manifest.Items[0].Caption != item.Caption {
		t.Fatalf("unexpected personal manifest: %+v", manifest)
	}
}

func TestExecuteRejectsDigestConflict(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "source.gif")
	data := []byte("GIF89a conflict source")
	writeFile(t, source, data)
	home := filepath.Join(parent, "home")
	item := itemForData(data, "wrong")
	item.SHA256 = strings.Repeat("0", sha256.Size*2)
	root, err := library.New(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.WriteManifest(context.Background(), library.Manifest{SchemaVersion: 1, Collection: "personal", Items: []library.Item{item}}); err != nil {
		t.Fatal(err)
	}
	_, err = Execute(context.Background(), Options{Home: home, Path: source})
	var coded *library.Error
	if !errors.As(err, &coded) || coded.Kind != "conflict" || coded.Subtype != "digest_conflict" {
		t.Fatalf("expected digest conflict, got %T %v", err, err)
	}
}

func TestExecuteDryRunDoesNotCreateHome(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "source.gif")
	writeFile(t, source, []byte("GIF89a dry run source"))
	home := filepath.Join(parent, "home")
	result, err := Execute(context.Background(), Options{Home: home, Path: source, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.DryRun || !result.Added || result.Item.Path != filepath.Join(home, filepath.FromSlash(result.Item.Filename)) {
		t.Fatalf("unexpected dry-run result: %+v", result)
	}
	if _, err := os.Stat(home); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run created home: %v", err)
	}
}

func TestExecuteConcurrentAddsKeepAllEntries(t *testing.T) {
	parent := t.TempDir()
	home := filepath.Join(parent, "home")
	const count = 8
	paths := make([]string, count)
	for i := range paths {
		paths[i] = filepath.Join(parent, "source-"+string(rune('a'+i))+".gif")
		writeFile(t, paths[i], []byte("GIF89a concurrent source "+string(rune('a'+i))))
	}
	var group sync.WaitGroup
	errorsOut := make(chan error, count)
	for _, path := range paths {
		group.Add(1)
		go func(path string) {
			defer group.Done()
			result, err := Execute(context.Background(), Options{Home: home, Path: path})
			if err == nil && !result.Added {
				errorsOut <- errors.New("concurrent add was not reported as added")
				return
			}
			errorsOut <- err
		}(path)
	}
	group.Wait()
	close(errorsOut)
	for err := range errorsOut {
		if err != nil {
			t.Fatal(err)
		}
	}
	root, err := library.New(home)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := root.ReadManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Items) != count {
		t.Fatalf("concurrent adds lost entries: got %d want %d", len(manifest.Items), count)
	}
}

func TestExecuteImageWriteFailureDoesNotPublishMissingEntry(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "source.gif")
	data := []byte("GIF89a write failure source")
	writeFile(t, source, data)
	home := filepath.Join(parent, "home")
	item := itemForData(data, "")
	if err := os.MkdirAll(filepath.Join(home, library.EmoticonsDirectory), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(home, filepath.FromSlash(item.Filename)), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := Execute(context.Background(), Options{Home: home, Path: source})
	if err == nil {
		t.Fatal("add unexpectedly succeeded with a directory at the target image path")
	}
	root, err := library.New(home)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := root.ReadManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Items) != 0 {
		t.Fatalf("failed image write published a manifest entry: %+v", manifest)
	}
}

func itemForData(data []byte, caption string) library.Item {
	md5Sum := md5.Sum(data)
	shaSum := sha256.Sum256(data)
	id := hex.EncodeToString(md5Sum[:])
	return library.Item{
		MD5:      id,
		SHA256:   hex.EncodeToString(shaSum[:]),
		Filename: filepath.ToSlash(filepath.Join(library.EmoticonsDirectory, id+".gif")),
		Format:   "gif",
		Size:     int64(len(data)),
		Caption:  caption,
	}
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
