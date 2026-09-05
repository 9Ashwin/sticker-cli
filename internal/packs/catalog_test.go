package packs

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/9Ashwin/sticker-cli/internal/library"
)

type catalogFixture struct {
	root          string
	allManifest   []byte
	curated       []byte
	allRevision   string
	curatedRev    string
	directory     []byte
	curatedSource string
}

func newCatalogFixture(t *testing.T) catalogFixture {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "packs"), 0o755); err != nil {
		t.Fatal(err)
	}
	allManifest := marshalManifest(t, "all_local", "all item", "gif")
	curatedManifest := marshalManifest(t, "curated", "curated item", "png")
	allRevision := hashText(allManifest)
	curatedRevision := hashText(curatedManifest)
	directory := directory{
		SchemaVersion: 1,
		Packs: []entry{
			{ID: "all", Name: "All", Description: "All local images", Manifest: "manifest.json", ManifestSHA256: allRevision, Count: 1, Size: manifestSize(allManifest)},
			{ID: "curated", Name: "Curated", Description: "A small selection", Manifest: "packs/curated.json", ManifestSHA256: curatedRevision, Count: 1, Size: manifestSize(curatedManifest)},
		},
	}
	directoryBytes, err := json.Marshal(directory)
	if err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, filepath.Join(root, "manifest.json"), allManifest)
	writeFixtureFile(t, filepath.Join(root, "packs", "curated.json"), curatedManifest)
	writeFixtureFile(t, filepath.Join(root, "packs.json"), directoryBytes)
	return catalogFixture{
		root:          root,
		allManifest:   allManifest,
		curated:       curatedManifest,
		allRevision:   allRevision,
		curatedRev:    curatedRevision,
		directory:     directoryBytes,
		curatedSource: filepath.Clean(root),
	}
}

func marshalManifest(t *testing.T, collection, caption, format string) []byte {
	t.Helper()
	content := []byte("fixture-" + collection)
	md5sum := md5.Sum(content)
	sha256sum := sha256.Sum256(content)
	manifest := library.Manifest{
		SchemaVersion: 1,
		Collection:    collection,
		Items: []library.Item{{
			MD5:      hex.EncodeToString(md5sum[:]),
			SHA256:   hex.EncodeToString(sha256sum[:]),
			Filename: "emoticons/" + hex.EncodeToString(md5sum[:]) + "." + format,
			Format:   format,
			Size:     int64(len(content)),
			Caption:  caption,
		}},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func manifestSize(data []byte) int64 {
	var manifest library.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		panic(err)
	}
	return manifest.Items[0].Size
}

func hashText(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func writeFixtureFile(t *testing.T, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(name, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverLocalAndOfflineCache(t *testing.T) {
	fixture := newCatalogFixture(t)
	home := t.TempDir()
	now := time.Date(2026, 9, 6, 1, 2, 3, 0, time.UTC)

	result, err := Discover(context.Background(), Options{
		Home:   home,
		Source: fixture.root,
		Now:    func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != fixture.root || result.FetchedAt != now || result.Stale {
		t.Fatalf("unexpected local result: %+v", result)
	}
	if got := packIDs(result.Items); fmt.Sprint(got) != "[all curated]" {
		t.Fatalf("items are not sorted by ID: %v", got)
	}

	stateDir := filepath.Join(home, ".sticker", "packs")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	state := installedState{
		SchemaVersion: 1,
		ID:            "curated",
		Source:        fixture.root,
		Revision:      fixture.curatedRev,
		InstalledAt:   now,
		Manifest:      json.RawMessage(fixture.curated),
	}
	stateBytes, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, filepath.Join(stateDir, "curated.json"), stateBytes)

	offline, err := Discover(context.Background(), Options{
		Home:    home,
		Source:  fixture.root,
		Offline: true,
		Now:     func() time.Time { return now.Add(cacheTTL + time.Second) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !offline.Stale || offline.FetchedAt != now {
		t.Fatalf("offline cache metadata is wrong: %+v", offline)
	}
	if !offline.Items[1].Installed || offline.Items[0].Installed {
		t.Fatalf("installed state is wrong: %+v", offline.Items)
	}

	_, err = Discover(context.Background(), Options{Home: home, Source: t.TempDir(), Offline: true})
	assertPackError(t, err, "not_found", "source_not_found")
}

func TestDiscoverHTTPSReadsOnlyCatalogAndManifests(t *testing.T) {
	fixture := newCatalogFixture(t)
	var mu sync.Mutex
	var requests []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/packs.json":
			_, _ = w.Write(fixture.directory)
		case "/manifest.json":
			_, _ = w.Write(fixture.allManifest)
		case "/packs/curated.json":
			_, _ = w.Write(fixture.curated)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	result, err := Discover(context.Background(), Options{
		Home:       t.TempDir(),
		Source:     server.URL + "/",
		HTTPClient: server.Client(),
		Backoff:    func(context.Context, time.Duration) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != server.URL || len(result.Items) != 2 {
		t.Fatalf("unexpected HTTPS result: %+v", result)
	}
	mu.Lock()
	got := append([]string(nil), requests...)
	mu.Unlock()
	sort.Strings(got)
	want := []string{"/manifest.json", "/packs.json", "/packs/curated.json"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("unexpected source requests: got %v want %v", got, want)
	}
}

func TestDiscoverRejectsUnsafeSourceAndManifest(t *testing.T) {
	for _, raw := range []string{
		"http://example.com/source",
		"https://example.com/source?token=secret",
		"https://user@example.com/source",
		"https://example.com/source/../other",
	} {
		_, err := Resolve(raw)
		assertPackError(t, err, "validation", "invalid_argument")
	}

	fixture := newCatalogFixture(t)
	unsafe := `{"schema_version":1,"packs":[{"id":"curated","name":"Curated","description":"unsafe","manifest":"../manifest.json","manifest_sha256":"` + strings.Repeat("0", 64) + `","count":0,"size":0}]}`
	writeFixtureFile(t, filepath.Join(fixture.root, "packs.json"), []byte(unsafe))
	_, err := Discover(context.Background(), Options{Home: t.TempDir(), Source: fixture.root})
	assertPackError(t, err, "validation", "unsafe_path")
}

func TestDiscoverCancellationAndInvalidCache(t *testing.T) {
	fixture := newCatalogFixture(t)
	home := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Discover(ctx, Options{Home: home, Source: fixture.root})
	assertPackError(t, err, "cancelled", "interrupted")

	source, err := Resolve(fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	cachePath := source.cachePath(home)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, cachePath, []byte(`{"schema_version":1,"source":"`+fixture.root+`","fetched_at":"2026-09-06T00:00:00Z","items":null}`))
	_, err = Discover(context.Background(), Options{Home: home, Source: fixture.root, Offline: true})
	assertPackError(t, err, "integrity", "invalid_collection")
}

func assertPackError(t *testing.T, err error, kind, subtype string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s/%s error", kind, subtype)
	}
	var packErr *Error
	if !errors.As(err, &packErr) {
		t.Fatalf("error is not packs.Error: %T %v", err, err)
	}
	if packErr.Kind != kind || packErr.Subtype != subtype {
		t.Fatalf("error = %s/%s, want %s/%s", packErr.Kind, packErr.Subtype, kind, subtype)
	}
}

func packIDs(items []Pack) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}
