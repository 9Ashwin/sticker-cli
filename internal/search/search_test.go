package search

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/9Ashwin/sticker-cli/internal/library"
)

func fixtureItem(label, caption string) library.Item {
	data := []byte("GIF89a-" + label)
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

func writePersonalManifest(t *testing.T, root string, items []library.Item) {
	t.Helper()
	personal, err := library.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := personal.WriteManifest(context.Background(), library.Manifest{SchemaVersion: 1, Collection: "personal", Items: items}); err != nil {
		t.Fatal(err)
	}
}

func writePackState(t *testing.T, root, id string, items []library.Item) {
	t.Helper()
	path := filepath.Join(root, packStateDirectory)
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	state := packState{
		SchemaVersion: 1,
		ID:            id,
		Source:        "file:///fixture",
		Revision:      "fixture-1",
		InstalledAt:   "2026-09-06T00:00:00Z",
		Manifest:      library.Manifest{SchemaVersion: 1, Collection: id, Items: items},
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, id+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestExecuteMergesSourcesAndPrefersPersonalCaption(t *testing.T) {
	root := t.TempDir()
	shared := fixtureItem("shared", "等待回应")
	shared.Caption = ""
	personalOnly := fixtureItem("personal", "调皮回应")
	alphaOnly := fixtureItem("alpha", "工作场景")
	betaOnly := fixtureItem("beta", "等待场景")
	writePersonalManifest(t, root, []library.Item{shared, personalOnly})
	packShared := shared
	packShared.Caption = "等待回应 from alpha"
	packBeta := betaOnly
	packBeta.Caption = "等待场景 from beta"
	writePackState(t, root, "alpha", []library.Item{packShared, alphaOnly})
	writePackState(t, root, "beta", []library.Item{shared, packBeta})

	result, err := Execute(context.Background(), Options{Home: root, Query: "场景", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 2 || len(result.Items) != 2 || result.HasMore {
		t.Fatalf("unexpected broad scene result: %+v", result)
	}
	if result.Items[0].ID >= result.Items[1].ID {
		t.Fatalf("results are not sorted by md5: %+v", result.Items)
	}
	for _, item := range result.Items {
		if item.ID == alphaOnly.MD5 && (item.Caption != "工作场景" || len(item.Packs) != 1 || item.Packs[0] != "alpha") {
			t.Fatalf("unexpected alpha item: %+v", item)
		}
		if item.ID == betaOnly.MD5 && (item.Caption != "等待场景 from beta" || len(item.Packs) != 1 || item.Packs[0] != "beta") {
			t.Fatalf("unexpected beta item: %+v", item)
		}
	}

	result, err = Execute(context.Background(), Options{Home: root, Query: "", Pack: "alpha", Favorites: true, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || len(result.Items) != 1 || result.Items[0].ID != shared.MD5 || !result.Items[0].Favorite || result.Items[0].Caption != "" {
		t.Fatalf("pack/favorite intersection did not preserve empty personal caption: %+v", result)
	}
}

func TestExecutePaginationAndEmptyResults(t *testing.T) {
	root := t.TempDir()
	items := []library.Item{
		fixtureItem("one", "WORK alpha"),
		fixtureItem("two", "work beta"),
		fixtureItem("three", "work gamma"),
	}
	writePersonalManifest(t, root, items)

	result, err := Execute(context.Background(), Options{Home: root, Query: "work", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 3 || len(result.Items) != 2 || result.NextOffset != 2 || !result.HasMore {
		t.Fatalf("unexpected first page: %+v", result)
	}
	firstPageLastID := result.Items[1].ID
	result, err = Execute(context.Background(), Options{Home: root, Query: "work", Limit: 2, Offset: result.NextOffset})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 3 || len(result.Items) != 1 || result.Items[0].ID == firstPageLastID || result.NextOffset != 3 || result.HasMore {
		t.Fatalf("unexpected second page: %+v", result)
	}
	result, err = Execute(context.Background(), Options{Home: root, Query: "missing", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 0 || len(result.Items) != 0 || result.HasMore {
		t.Fatalf("empty search should succeed with an empty page: %+v", result)
	}
	result, err = Execute(context.Background(), Options{Home: root, Query: "work", Limit: 10, Offset: 99})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 3 || len(result.Items) != 0 || result.NextOffset != 99 || result.HasMore {
		t.Fatalf("out-of-range offset should be resumable and empty: %+v", result)
	}
}

func TestExecuteMarksEmptyLibraryForSetup(t *testing.T) {
	result, err := Execute(context.Background(), Options{Home: t.TempDir(), Query: "调皮", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 0 || len(result.Items) != 0 || !result.SetupRequired {
		t.Fatalf("empty library should request setup: %+v", result)
	}
}

func TestExecuteDoesNotRequestSetupWhenFavoritesExist(t *testing.T) {
	root := t.TempDir()
	writePersonalManifest(t, root, []library.Item{fixtureItem("personal", "调皮回应")})
	result, err := Execute(context.Background(), Options{Home: root, Query: "调皮", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || result.SetupRequired {
		t.Fatalf("personal library should be ready without a pack: %+v", result)
	}
}

func TestExecuteRejectsDigestConflictAndUnknownPack(t *testing.T) {
	root := t.TempDir()
	first := fixtureItem("conflict", "first")
	second := fixtureItem("other", "second")
	second.MD5 = first.MD5
	second.Filename = first.Filename
	writePersonalManifest(t, root, []library.Item{first})
	writePackState(t, root, "alpha", []library.Item{second})
	_, err := Execute(context.Background(), Options{Home: root, Query: "", Limit: 10})
	var coded *library.Error
	if !errors.As(err, &coded) || coded.Kind != "conflict" || coded.Subtype != "digest_conflict" {
		t.Fatalf("expected digest conflict, got %v", err)
	}
	_, err = Execute(context.Background(), Options{Home: root, Query: "", Pack: "missing", Limit: 10})
	if !errors.As(err, &coded) || coded.Kind != "not_found" || coded.Subtype != "pack_not_found" {
		t.Fatalf("expected missing pack error, got %v", err)
	}
}

func TestExecuteReadsOnlyLocalPackState(t *testing.T) {
	root := t.TempDir()
	item := fixtureItem("offline", "offline scene")
	writePackState(t, root, "offline", []library.Item{item})
	start := time.Now()
	result, err := Execute(context.Background(), Options{Home: root, Query: "offline", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || len(result.Items) != 1 {
		t.Fatalf("unexpected offline result: %+v", result)
	}
	t.Logf("offline search over one local item completed in %s", time.Since(start).Round(time.Microsecond))
}

func TestExecuteBaseline2638Items(t *testing.T) {
	root := t.TempDir()
	items := make([]library.Item, 2638)
	for index := range items {
		id := fmt.Sprintf("%032x", index+1)
		sha := fmt.Sprintf("%064x", index+1)
		items[index] = library.Item{
			MD5:      id,
			SHA256:   sha,
			Filename: "emoticons/" + id + ".gif",
			Format:   "gif",
			Size:     1,
			Caption:  fmt.Sprintf("scene %d", index),
		}
	}
	writePersonalManifest(t, root, items)
	start := time.Now()
	result, err := Execute(context.Background(), Options{Home: root, Query: "scene", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != len(items) || len(result.Items) != 100 || !result.HasMore {
		t.Fatalf("baseline search returned unexpected page: total=%d items=%d has_more=%t", result.Total, len(result.Items), result.HasMore)
	}
	t.Logf("offline search over %d manifest items completed in %s", len(items), time.Since(start).Round(time.Microsecond))
}
