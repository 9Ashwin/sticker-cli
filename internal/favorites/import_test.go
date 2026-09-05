package favorites

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/9Ashwin/sticker-cli/internal/library"
)

func TestImportMergesV1DirectoryWithoutPacksAndPreservesCaption(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "source")
	home := filepath.Join(parent, "home")
	firstData := []byte("GIF89a imported first")
	secondData := []byte("GIF89a imported second")
	first := itemForData(firstData, "source first")
	second := itemForData(secondData, "source second")
	writeItem(t, source, first, firstData)
	writeItem(t, source, second, secondData)
	writeManifest(t, source, "shared", first, second)

	personal := first
	personal.Caption = "personal caption"
	writeItem(t, home, personal, firstData)
	writeManifest(t, home, "personal", personal)

	result, err := Import(context.Background(), ImportOptions{Home: home, Source: source})
	if err != nil {
		t.Fatal(err)
	}
	if result.Added != 1 || result.Skipped != 1 || result.Updated != 0 || result.Conflicts != 0 || result.Failed != 0 || !result.Committed {
		t.Fatalf("unexpected import result: %+v", result)
	}

	manifest := readManifest(t, home)
	if manifest.Collection != "personal" || len(manifest.Items) != 2 {
		t.Fatalf("unexpected personal manifest: %+v", manifest)
	}
	got := itemByID(manifest.Items, first.MD5)
	if got.Caption != personal.Caption {
		t.Fatalf("target caption was replaced: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(home, filepath.FromSlash(second.Filename))); err != nil {
		t.Fatalf("imported image is missing: %v", err)
	}
	if err := os.RemoveAll(source); err != nil {
		t.Fatal(err)
	}
	root, err := library.New(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Validate(context.Background(), manifest); err != nil {
		t.Fatalf("personal library is not usable after source removal: %v", err)
	}
}

func TestImportOverwriteCaptionsAndDryRun(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "source")
	home := filepath.Join(parent, "home")
	data := []byte("GIF89a overwrite caption")
	sourceItem := itemForData(data, "source caption")
	writeItem(t, source, sourceItem, data)
	writeManifest(t, source, "shared", sourceItem)

	personal := sourceItem
	personal.Caption = "target caption"
	writeItem(t, home, personal, data)
	writeManifest(t, home, "personal", personal)

	dryRun, err := Import(context.Background(), ImportOptions{Home: home, Source: source, OverwriteCaptions: true, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if dryRun.Added != 0 || dryRun.Skipped != 0 || dryRun.Updated != 1 || dryRun.Committed || !dryRun.DryRun {
		t.Fatalf("unexpected dry-run result: %+v", dryRun)
	}
	if got := itemByID(readManifest(t, home).Items, sourceItem.MD5).Caption; got != "target caption" {
		t.Fatalf("dry-run changed caption: %q", got)
	}

	result, err := Import(context.Background(), ImportOptions{Home: home, Source: source, OverwriteCaptions: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Added != 0 || result.Skipped != 0 || result.Updated != 1 || !result.Committed {
		t.Fatalf("unexpected overwrite result: %+v", result)
	}
	if got := itemByID(readManifest(t, home).Items, sourceItem.MD5).Caption; got != "source caption" {
		t.Fatalf("caption was not overwritten: %q", got)
	}
}

func TestImportSourceEqualsTargetIsIdempotent(t *testing.T) {
	root := t.TempDir()
	data := []byte("GIF89a same source and target")
	item := itemForData(data, "keep this")
	writeItem(t, root, item, data)
	writeManifest(t, root, "personal", item)

	result, err := Import(context.Background(), ImportOptions{Home: root, Source: root})
	if err != nil {
		t.Fatal(err)
	}
	if result.Added != 0 || result.Skipped != 1 || result.Updated != 0 || !result.Committed {
		t.Fatalf("same-library import was not idempotent: %+v", result)
	}
	if got := itemByID(readManifest(t, root).Items, item.MD5).Caption; got != item.Caption {
		t.Fatalf("same-library import changed caption: %q", got)
	}
}

func TestImportFailureDoesNotPublishManifest(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "source")
	home := filepath.Join(parent, "home")
	validData := []byte("GIF89a valid before missing")
	valid := itemForData(validData, "valid")
	missing := itemForData([]byte("GIF89a missing"), "missing")
	writeItem(t, source, valid, validData)
	writeManifest(t, source, "broken", valid, missing)

	result, err := Import(context.Background(), ImportOptions{Home: home, Source: source})
	if err == nil {
		t.Fatal("import unexpectedly succeeded with a missing source image")
	}
	if result.Committed || result.Failed == 0 {
		t.Fatalf("failed import did not report an uncommitted failure: %+v", result)
	}
	if _, statErr := os.Stat(filepath.Join(home, library.ManifestName)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed import published a manifest: %v", statErr)
	}
}

func TestImportRejectsConflictAndUnsafeManifest(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "source")
	home := filepath.Join(parent, "home")
	data := []byte("GIF89a conflict import")
	sourceItem := itemForData(data, "source")
	writeItem(t, source, sourceItem, data)
	writeManifest(t, source, "shared", sourceItem)

	conflicting := sourceItem
	conflicting.SHA256 = strings.Repeat("0", 64)
	writeManifest(t, home, "personal", conflicting)
	before := readManifestBytes(t, home)
	result, err := Import(context.Background(), ImportOptions{Home: home, Source: source})
	var coded *library.Error
	if !errors.As(err, &coded) || coded.Kind != "conflict" || coded.Subtype != "digest_conflict" {
		t.Fatalf("expected digest conflict, got %T %v", err, err)
	}
	if result.Conflicts != 1 || result.Committed || result.Failed != 1 {
		t.Fatalf("unexpected conflict result: %+v", result)
	}
	if got := readManifestBytes(t, home); string(got) != string(before) {
		t.Fatalf("conflict changed personal manifest")
	}

	unsafeSource := filepath.Join(parent, "unsafe")
	if err := os.MkdirAll(unsafeSource, 0o700); err != nil {
		t.Fatal(err)
	}
	unsafe := sourceItem
	unsafe.Filename = "../outside.gif"
	dataBytes, err := json.Marshal(library.Manifest{SchemaVersion: 1, Collection: "shared", Items: []library.Item{unsafe}})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(unsafeSource, library.ManifestName), dataBytes)
	_, err = Import(context.Background(), ImportOptions{Home: filepath.Join(parent, "unsafe-home"), Source: unsafeSource})
	if !errors.As(err, &coded) || coded.Kind != "integrity" || coded.Subtype != "invalid_manifest" {
		t.Fatalf("expected unsafe manifest rejection, got %T %v", err, err)
	}
}

func TestImportConcurrentFavoriteIsRetained(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "source")
	home := filepath.Join(parent, "home")
	data := append([]byte("GIF89a large import "), make([]byte, 8<<20)...)
	item := itemForData(data, "imported")
	writeItem(t, source, item, data)
	writeManifest(t, source, "shared", item)
	favoritePath := filepath.Join(parent, "favorite.gif")
	favoriteData := []byte("GIF89a concurrent favorite")
	writeFile(t, favoritePath, favoriteData)

	start := make(chan struct{})
	var group sync.WaitGroup
	group.Add(1)
	var addErr error
	go func() {
		defer group.Done()
		<-start
		_, addErr = Execute(context.Background(), Options{Home: home, Path: favoritePath})
	}()
	close(start)
	result, importErr := Import(context.Background(), ImportOptions{Home: home, Source: source})
	group.Wait()
	if importErr != nil {
		t.Fatal(importErr)
	}
	if addErr != nil {
		t.Fatal(addErr)
	}
	manifest := readManifest(t, home)
	if len(manifest.Items) != 2 {
		t.Fatalf("concurrent favorite was lost: result=%+v manifest=%+v", result, manifest)
	}
}

func writeManifest(t *testing.T, root, collection string, items ...library.Item) {
	t.Helper()
	libraryRoot, err := library.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := libraryRoot.WriteManifest(context.Background(), library.Manifest{SchemaVersion: 1, Collection: collection, Items: items}); err != nil {
		t.Fatal(err)
	}
}

func writeItem(t *testing.T, root string, item library.Item, data []byte) {
	t.Helper()
	writeFile(t, filepath.Join(root, filepath.FromSlash(item.Filename)), data)
}

func readManifest(t *testing.T, root string) library.Manifest {
	t.Helper()
	libraryRoot, err := library.New(root)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := libraryRoot.ReadManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func readManifestBytes(t *testing.T, root string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, library.ManifestName))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func itemByID(items []library.Item, id string) library.Item {
	for _, item := range items {
		if item.MD5 == id {
			return item
		}
	}
	return library.Item{}
}
