package packs

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/9Ashwin/sticker-cli/internal/library"
)

type installFixture struct {
	root      string
	item      library.Item
	content   []byte
	manifest  []byte
	directory []byte
	revision  string
}

func newInstallFixture(t *testing.T) installFixture {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "packs"), 0o700); err != nil {
		t.Fatal(err)
	}
	content := []byte("GIF89a install fixture")
	md5sum := md5.Sum(content)
	shaSum := sha256.Sum256(content)
	md5Text := hex.EncodeToString(md5sum[:])
	item := library.Item{
		MD5:      md5Text,
		SHA256:   hex.EncodeToString(shaSum[:]),
		Filename: "emoticons/" + md5Text + ".gif",
		Format:   "gif",
		Size:     int64(len(content)),
		Caption:  "install fixture",
	}
	manifest := library.Manifest{SchemaVersion: 1, Collection: "curated", Items: []library.Item{item}}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	revision := hashText(manifestBytes)
	directory := directory{SchemaVersion: 1, Packs: []entry{{
		ID:             "curated",
		Name:           "Curated",
		Description:    "A small selection",
		Manifest:       "packs/curated.json",
		ManifestSHA256: revision,
		Count:          1,
		Size:           item.Size,
	}}}
	directoryBytes, err := json.Marshal(directory)
	if err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, filepath.Join(root, "packs.json"), directoryBytes)
	writeFixtureFile(t, filepath.Join(root, "packs", "curated.json"), manifestBytes)
	if err := os.MkdirAll(filepath.Join(root, library.EmoticonsDirectory), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, filepath.Join(root, filepath.FromSlash(item.Filename)), content)
	return installFixture{root: root, item: item, content: content, manifest: manifestBytes, directory: directoryBytes, revision: revision}
}

func TestPlanCountsMissingAndVerifiedImages(t *testing.T) {
	fixture := newInstallFixture(t)
	missingHome := filepath.Join(t.TempDir(), "library")
	plan, err := Plan(context.Background(), PlanOptions{Home: missingHome, Source: fixture.root, PackID: "curated"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Source != fixture.root || plan.Target != missingHome || plan.Revision != fixture.revision || plan.Added != 1 || plan.Reused != 0 || plan.DownloadBytes != fixture.item.Size {
		t.Fatalf("unexpected missing-image plan: %+v", plan)
	}
	if _, err := os.Stat(missingHome); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("planning created the home directory: %v", err)
	}

	reusedHome := t.TempDir()
	imagePath := filepath.Join(reusedHome, filepath.FromSlash(fixture.item.Filename))
	if err := os.MkdirAll(filepath.Dir(imagePath), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, imagePath, fixture.content)
	plan, err = Plan(context.Background(), PlanOptions{Home: reusedHome, Source: fixture.root, PackID: "curated"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Added != 0 || plan.Reused != 1 || plan.DownloadBytes != 0 {
		t.Fatalf("unexpected reused-image plan: %+v", plan)
	}
}

func TestPlanHTTPSDoesNotDownloadImagesOrWriteCache(t *testing.T) {
	fixture := newInstallFixture(t)
	var mu sync.Mutex
	var requests []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.URL.Path)
		mu.Unlock()
		switch r.URL.Path {
		case "/packs.json":
			_, _ = w.Write(fixture.directory)
		case "/packs/curated.json":
			_, _ = w.Write(fixture.manifest)
		default:
			http.Error(w, "image download is not part of planning", http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)
	home := filepath.Join(t.TempDir(), "library")
	plan, err := Plan(context.Background(), PlanOptions{
		Home:       home,
		Source:     server.URL,
		PackID:     "curated",
		HTTPClient: server.Client(),
		Backoff:    func(context.Context, time.Duration) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Added != 1 || plan.DownloadBytes != fixture.item.Size {
		t.Fatalf("unexpected HTTPS plan: %+v", plan)
	}
	mu.Lock()
	got := append([]string(nil), requests...)
	mu.Unlock()
	if strings.Contains(strings.Join(got, "\n"), "emoticons/") {
		t.Fatalf("planning requested an image: %v", got)
	}
	if len(got) != 2 {
		t.Fatalf("unexpected planning requests: %v", got)
	}
	if _, err := os.Stat(home); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("planning created the home directory: %v", err)
	}
	cacheDir := filepath.Join(home, ".sticker", "catalogs")
	if _, err := os.Stat(cacheDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("planning created a catalog cache: %v", err)
	}
}

func TestPlanRejectsDamagedExistingImage(t *testing.T) {
	fixture := newInstallFixture(t)
	home := t.TempDir()
	imagePath := filepath.Join(home, filepath.FromSlash(fixture.item.Filename))
	if err := os.MkdirAll(filepath.Dir(imagePath), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, imagePath, []byte("GIF89a damaged"))
	_, err := Plan(context.Background(), PlanOptions{Home: home, Source: fixture.root, PackID: "curated"})
	assertPackError(t, err, "integrity", "hash_mismatch")
}

func TestPlanRejectsPersonalDigestConflict(t *testing.T) {
	fixture := newInstallFixture(t)
	home := t.TempDir()
	conflict := fixture.item
	conflict.SHA256 = strings.Repeat("0", sha256.Size*2)
	manifest := library.Manifest{SchemaVersion: 1, Collection: "personal", Items: []library.Item{conflict}}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, filepath.Join(home, library.ManifestName), data)
	_, err = Plan(context.Background(), PlanOptions{Home: home, Source: fixture.root, PackID: "curated"})
	assertPackError(t, err, "conflict", "digest_conflict")
}

func TestPlanRejectsInstalledSourceAndRevisionConflicts(t *testing.T) {
	fixture := newInstallFixture(t)
	otherSource := newInstallFixture(t)
	oldManifest := append([]byte(nil), fixture.manifest...)
	var old library.Manifest
	if err := json.Unmarshal(oldManifest, &old); err != nil {
		t.Fatal(err)
	}
	old.Collection = "old"
	oldManifest, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		source   string
		revision string
		manifest []byte
		wantKind string
		wantSub  string
	}{
		{name: "source", source: otherSource.root, revision: fixture.revision, manifest: fixture.manifest, wantKind: "conflict", wantSub: "source_conflict"},
		{name: "revision", source: fixture.root, revision: hashText(oldManifest), manifest: oldManifest, wantKind: "conflict", wantSub: "state_changed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			stateDir := filepath.Join(home, ".sticker", "packs")
			if err := os.MkdirAll(stateDir, 0o700); err != nil {
				t.Fatal(err)
			}
			state := installedState{SchemaVersion: 1, ID: "curated", Source: test.source, Revision: test.revision, InstalledAt: time.Now().UTC(), Manifest: test.manifest}
			data, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			writeFixtureFile(t, filepath.Join(stateDir, "curated.json"), data)
			_, err = Plan(context.Background(), PlanOptions{Home: home, Source: fixture.root, PackID: "curated"})
			assertPackError(t, err, test.wantKind, test.wantSub)
		})
	}
}

func TestPlanUsesRawManifestRevisionAndRejectsDirectoryDrift(t *testing.T) {
	fixture := newInstallFixture(t)
	home := filepath.Join(t.TempDir(), "library")
	first, err := Plan(context.Background(), PlanOptions{Home: home, Source: fixture.root, PackID: "curated"})
	if err != nil {
		t.Fatal(err)
	}
	var manifest library.Manifest
	if err := json.Unmarshal(fixture.manifest, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Items[0].Caption = "source changed"
	changedManifest, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	changedRevision := hashText(changedManifest)
	if changedRevision == first.Revision {
		t.Fatal("changing manifest bytes did not change the revision")
	}
	writeFixtureFile(t, filepath.Join(fixture.root, "packs", "curated.json"), changedManifest)
	var changedDirectory directory
	if err := json.Unmarshal(fixture.directory, &changedDirectory); err != nil {
		t.Fatal(err)
	}
	changedDirectory.Packs[0].ManifestSHA256 = changedRevision
	changedBytes, err := json.Marshal(changedDirectory)
	if err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, filepath.Join(fixture.root, "packs.json"), changedBytes)
	second, err := Plan(context.Background(), PlanOptions{Home: home, Source: fixture.root, PackID: "curated"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Revision != changedRevision || second.Pack.Count != 1 || second.Pack.Size != fixture.item.Size {
		t.Fatalf("plan did not use the changed raw manifest revision: %+v", second)
	}

	changedDirectory.Packs[0].Count = 2
	driftedBytes, err := json.Marshal(changedDirectory)
	if err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, filepath.Join(fixture.root, "packs.json"), driftedBytes)
	_, err = Plan(context.Background(), PlanOptions{Home: home, Source: fixture.root, PackID: "curated"})
	assertPackError(t, err, "integrity", "hash_mismatch")
}

func TestPlanRequiresKnownPackID(t *testing.T) {
	fixture := newInstallFixture(t)
	for _, id := range []string{"", "missing", "../escape"} {
		_, err := Plan(context.Background(), PlanOptions{Home: t.TempDir(), Source: fixture.root, PackID: id})
		if id == "missing" {
			assertPackError(t, err, "not_found", "pack_not_found")
		} else {
			assertPackError(t, err, "validation", "invalid_argument")
		}
	}
}

func TestInstallCopiesOriginalAndPublishesState(t *testing.T) {
	fixture := newInstallFixture(t)
	home := filepath.Join(t.TempDir(), "library")
	result, err := Install(context.Background(), InstallOptions{Home: home, Source: fixture.root, PackID: "curated"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Added != 1 || result.Reused != 0 || result.DownloadBytes != fixture.item.Size || !result.Pack.Installed {
		t.Fatalf("unexpected install result: %+v", result)
	}
	data, err := os.ReadFile(filepath.Join(home, filepath.FromSlash(fixture.item.Filename)))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(fixture.content) {
		t.Fatalf("installed image changed: %q", data)
	}
	if _, err := os.Stat(filepath.Join(home, library.ManifestName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("install unexpectedly changed the personal manifest: %v", err)
	}
	state, installed, err := readInstalledState(home, "curated")
	if err != nil || !installed || state.Revision != fixture.revision {
		t.Fatalf("installed state was not published: %+v, %v, %v", state, installed, err)
	}

	repeated, err := Install(context.Background(), InstallOptions{Home: home, Source: fixture.root, PackID: "curated"})
	if err != nil {
		t.Fatal(err)
	}
	if repeated.Added != 0 || repeated.Reused != 1 || repeated.DownloadBytes != 0 {
		t.Fatalf("repeat install did not reuse the original: %+v", repeated)
	}
}

func TestInstallHTTPSFetchesOnlySelectedPackImages(t *testing.T) {
	fixture := newInstallFixture(t)
	allContent := []byte("GIF89a all-only fixture")
	allMD5 := md5.Sum(allContent)
	allSHA := sha256.Sum256(allContent)
	allID := hex.EncodeToString(allMD5[:])
	allItem := library.Item{MD5: allID, SHA256: hex.EncodeToString(allSHA[:]), Filename: "emoticons/" + allID + ".gif", Format: "gif", Size: int64(len(allContent)), Caption: "all only"}
	allManifest, err := json.Marshal(library.Manifest{SchemaVersion: 1, Collection: "all", Items: []library.Item{fixture.item, allItem}})
	if err != nil {
		t.Fatal(err)
	}
	allRevision := hashText(allManifest)
	var directory directory
	if err := json.Unmarshal(fixture.directory, &directory); err != nil {
		t.Fatal(err)
	}
	directory.Packs = append(directory.Packs, entry{ID: "all", Name: "All", Description: "Everything", Manifest: "manifest.json", ManifestSHA256: allRevision, Count: 2, Size: fixture.item.Size + allItem.Size})
	directoryBytes, err := json.Marshal(directory)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var requests []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.URL.Path)
		mu.Unlock()
		switch r.URL.Path {
		case "/packs.json":
			_, _ = w.Write(directoryBytes)
		case "/packs/curated.json":
			_, _ = w.Write(fixture.manifest)
		case "/emoticons/" + fixture.item.MD5 + ".gif":
			_, _ = w.Write(fixture.content)
		case "/manifest.json":
			_, _ = w.Write(allManifest)
		case "/emoticons/" + allItem.MD5 + ".gif":
			t.Errorf("selected curated install requested all-only image")
			_, _ = w.Write(allContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	home := filepath.Join(t.TempDir(), "library")
	result, err := Install(context.Background(), InstallOptions{
		Home:       home,
		Source:     server.URL,
		PackID:     "curated",
		HTTPClient: server.Client(),
		Backoff:    func(context.Context, time.Duration) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Added != 1 || result.DownloadBytes != fixture.item.Size {
		t.Fatalf("unexpected HTTPS install result: %+v", result)
	}
	mu.Lock()
	got := append([]string(nil), requests...)
	mu.Unlock()
	if !containsPath(got, "/packs.json") || !containsPath(got, "/packs/curated.json") || !containsPath(got, "/emoticons/"+fixture.item.MD5+".gif") {
		t.Fatalf("selected install did not request expected files: %v", got)
	}
	if containsPath(got, "/manifest.json") || containsPath(got, "/emoticons/"+allItem.MD5+".gif") {
		t.Fatalf("selected install accessed full-pack files: %v", got)
	}
}

func TestInstallFailureRetainsVerifiedStagingForRetry(t *testing.T) {
	firstContent := []byte("GIF89a first staged")
	secondContent := []byte("GIF89a second staged")
	first := makeItem(firstContent, "first")
	second := makeItem(secondContent, "second")
	manifestBytes, err := json.Marshal(library.Manifest{SchemaVersion: 1, Collection: "curated", Items: []library.Item{first, second}})
	if err != nil {
		t.Fatal(err)
	}
	revision := hashText(manifestBytes)
	directoryBytes, err := json.Marshal(directory{SchemaVersion: 1, Packs: []entry{{ID: "curated", Name: "Curated", Description: "retry", Manifest: "packs/curated.json", ManifestSHA256: revision, Count: 2, Size: first.Size + second.Size}}})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var requests []string
	serveSecond := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.URL.Path)
		ready := serveSecond
		mu.Unlock()
		switch r.URL.Path {
		case "/packs.json":
			_, _ = w.Write(directoryBytes)
		case "/packs/curated.json":
			_, _ = w.Write(manifestBytes)
		case "/emoticons/" + first.MD5 + ".gif":
			_, _ = w.Write(firstContent)
		case "/emoticons/" + second.MD5 + ".gif":
			if !ready {
				http.Error(w, "temporary failure", http.StatusServiceUnavailable)
				return
			}
			_, _ = w.Write(secondContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	home := filepath.Join(t.TempDir(), "library")
	options := InstallOptions{Home: home, Source: server.URL, PackID: "curated", HTTPClient: server.Client(), Backoff: func(context.Context, time.Duration) error { return nil }}
	if _, err := Install(context.Background(), options); err == nil {
		t.Fatal("failed image transfer unexpectedly succeeded")
	}
	if _, installed, err := readInstalledState(home, "curated"); err != nil || installed {
		t.Fatalf("failed install published state: %v, %v", err, installed)
	}
	mu.Lock()
	firstRequests := countPath(requests, "/emoticons/"+first.MD5+".gif")
	mu.Unlock()
	if firstRequests != 1 {
		t.Fatalf("expected one successful first-image request, got %d", firstRequests)
	}
	mu.Lock()
	serveSecond = true
	mu.Unlock()
	result, err := Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Added != 2 || result.DownloadBytes != second.Size {
		t.Fatalf("retry did not reuse staged image: %+v", result)
	}
	mu.Lock()
	firstRequests = countPath(requests, "/emoticons/"+first.MD5+".gif")
	secondRequests := countPath(requests, "/emoticons/"+second.MD5+".gif")
	mu.Unlock()
	if firstRequests != 1 || secondRequests != 4 {
		t.Fatalf("unexpected retry requests: first=%d second=%d all=%v", firstRequests, secondRequests, requests)
	}
}

func TestInstallCancellationLeavesStateUnpublished(t *testing.T) {
	fixture := newInstallFixture(t)
	started := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/packs.json":
			_, _ = w.Write(fixture.directory)
		case "/packs/curated.json":
			_, _ = w.Write(fixture.manifest)
		case "/emoticons/" + fixture.item.MD5 + ".gif":
			close(started)
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	home := filepath.Join(t.TempDir(), "library")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := Install(ctx, InstallOptions{Home: home, Source: server.URL, PackID: "curated", HTTPClient: server.Client(), Backoff: func(context.Context, time.Duration) error { return nil }})
		result <- err
	}()
	select {
	case <-started:
		cancel()
	case <-time.After(5 * time.Second):
		t.Fatal("image request did not start")
	}
	select {
	case err := <-result:
		assertPackError(t, err, "cancelled", "interrupted")
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled install did not return")
	}
	if _, installed, err := readInstalledState(home, "curated"); err != nil || installed {
		t.Fatalf("cancelled install published state: %v, %v", err, installed)
	}
}

func TestConcurrentInstallPublishesOneState(t *testing.T) {
	fixture := newInstallFixture(t)
	home := filepath.Join(t.TempDir(), "library")
	options := InstallOptions{Home: home, Source: fixture.root, PackID: "curated"}
	results := make(chan InstallResult, 2)
	errors := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			result, err := Install(context.Background(), options)
			results <- result
			errors <- err
		}()
	}
	group.Wait()
	close(results)
	close(errors)
	var added, reused int
	for result := range results {
		added += result.Added
		reused += result.Reused
	}
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if added != 1 || reused != 1 {
		t.Fatalf("concurrent installs did not re-read state: added=%d reused=%d", added, reused)
	}
	if _, installed, err := readInstalledState(home, "curated"); err != nil || !installed {
		t.Fatalf("concurrent install did not publish state: %v, %v", err, installed)
	}
}

func makeItem(content []byte, caption string) library.Item {
	md5Sum := md5.Sum(content)
	shaSum := sha256.Sum256(content)
	id := hex.EncodeToString(md5Sum[:])
	return library.Item{MD5: id, SHA256: hex.EncodeToString(shaSum[:]), Filename: "emoticons/" + id + ".gif", Format: "gif", Size: int64(len(content)), Caption: caption}
}

func containsPath(paths []string, target string) bool {
	return countPath(paths, target) > 0
}

func countPath(paths []string, target string) int {
	count := 0
	for _, path := range paths {
		if path == target {
			count++
		}
	}
	return count
}
