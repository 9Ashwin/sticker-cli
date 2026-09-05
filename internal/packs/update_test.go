package packs

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/9Ashwin/sticker-cli/internal/library"
)

func TestUpdateUsesSavedSourceAndPreservesPersonalManifest(t *testing.T) {
	fixture := newInstallFixture(t)
	home := filepath.Join(t.TempDir(), "library")
	if _, err := Install(context.Background(), InstallOptions{Home: home, Source: fixture.root, PackID: "curated"}); err != nil {
		t.Fatal(err)
	}

	personal := library.Manifest{
		SchemaVersion: 1,
		Collection:    "personal",
		Items: []library.Item{{
			MD5: fixture.item.MD5, SHA256: fixture.item.SHA256, Filename: fixture.item.Filename,
			Format: fixture.item.Format, Size: fixture.item.Size, Caption: "my favorite",
		}},
	}
	root, err := library.New(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.WriteManifest(context.Background(), personal); err != nil {
		t.Fatal(err)
	}

	newItem := makeItem([]byte("GIF89a update fixture"), "new source image")
	changedItem := fixture.item
	changedItem.Caption = "source caption changed"
	manifestBytes := writeUpdatedPackFixture(t, fixture, []library.Item{changedItem, newItem}, [][]byte{nil, []byte("GIF89a update fixture")})
	newRevision := hashText(manifestBytes)

	stagingDir := filepath.Join(home, ".sticker", "staging")
	beforeStaging, err := os.ReadDir(stagingDir)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanUpdate(context.Background(), UpdateOptions{Home: home, PackID: "curated"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Source != fixture.root || plan.Revision != newRevision || plan.Added != 1 || plan.Reused != 1 || plan.DownloadBytes != newItem.Size {
		t.Fatalf("unexpected update plan: %+v", plan)
	}
	afterStaging, err := os.ReadDir(stagingDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if len(afterStaging) != len(beforeStaging) {
		t.Fatalf("dry-run planning changed staging files: before=%v after=%v", beforeStaging, afterStaging)
	}

	result, err := Update(context.Background(), UpdateOptions{Home: home, PackID: "curated"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != fixture.root || result.Revision != newRevision || result.Added != 1 || result.Reused != 1 || result.DownloadBytes != newItem.Size || !result.Pack.Installed {
		t.Fatalf("unexpected update result: %+v", result)
	}
	state, installed, err := readInstalledState(home, "curated")
	if err != nil || !installed || state.Revision != newRevision || string(state.Manifest) != string(manifestBytes) {
		t.Fatalf("updated state is wrong: %+v, %v, %v", state, installed, err)
	}
	personalAfter, err := root.ReadManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(personalAfter.Items) != 1 || personalAfter.Items[0].Caption != "my favorite" {
		t.Fatalf("update changed personal caption: %+v", personalAfter)
	}
	data, err := os.ReadFile(filepath.Join(home, filepath.FromSlash(newItem.Filename)))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "GIF89a update fixture" {
		t.Fatalf("updated image content changed: %q", data)
	}
}

func TestUpdateFailureLeavesPreviousStateUsable(t *testing.T) {
	fixture := newInstallFixture(t)
	home := filepath.Join(t.TempDir(), "library")
	if _, err := Install(context.Background(), InstallOptions{Home: home, Source: fixture.root, PackID: "curated"}); err != nil {
		t.Fatal(err)
	}
	oldState, installed, err := readInstalledState(home, "curated")
	if err != nil || !installed {
		t.Fatalf("initial state missing: %+v, %v, %v", oldState, installed, err)
	}
	oldImage, err := os.ReadFile(filepath.Join(home, filepath.FromSlash(fixture.item.Filename)))
	if err != nil {
		t.Fatal(err)
	}

	newItem := makeItem([]byte("GIF89a missing update"), "missing source image")
	changedItem := fixture.item
	changedItem.Caption = "changed source"
	_ = writeUpdatedPackFixture(t, fixture, []library.Item{changedItem, newItem}, [][]byte{nil, nil})

	_, err = Update(context.Background(), UpdateOptions{Home: home, PackID: "curated"})
	assertPackError(t, err, "not_found", "source_not_found")
	currentState, currentInstalled, err := readInstalledState(home, "curated")
	if err != nil || !currentInstalled || currentState.Revision != oldState.Revision || string(currentState.Manifest) != string(oldState.Manifest) {
		t.Fatalf("failed update changed installed state: %+v, %v, %v", currentState, currentInstalled, err)
	}
	currentImage, err := os.ReadFile(filepath.Join(home, filepath.FromSlash(fixture.item.Filename)))
	if err != nil {
		t.Fatal(err)
	}
	if string(currentImage) != string(oldImage) {
		t.Fatalf("failed update changed old image: %q", currentImage)
	}
}

func TestUpdateRequiresInstalledPack(t *testing.T) {
	_, err := Update(context.Background(), UpdateOptions{Home: filepath.Join(t.TempDir(), "library"), PackID: "curated"})
	assertPackError(t, err, "not_found", "pack_not_found")
}

func TestUpdateRejectsConcurrentRevisionChange(t *testing.T) {
	fixture := newInstallFixture(t)
	home := filepath.Join(t.TempDir(), "library")
	if _, err := Install(context.Background(), InstallOptions{Home: home, Source: fixture.root, PackID: "curated"}); err != nil {
		t.Fatal(err)
	}
	newItem := makeItem([]byte("GIF89a concurrent update"), "concurrent source image")
	_ = writeUpdatedPackFixture(t, fixture, []library.Item{fixture.item, newItem}, [][]byte{nil, []byte("GIF89a concurrent update")})
	prepared, err := prepareUpdate(context.Background(), UpdateOptions{Home: home, PackID: "curated"})
	if err != nil {
		t.Fatal(err)
	}

	state, installed, err := readInstalledState(home, "curated")
	if err != nil || !installed {
		t.Fatal("installed state is missing")
	}
	changedManifest, err := json.Marshal(library.Manifest{
		SchemaVersion: 1,
		Collection:    "curated",
		Items: []library.Item{{
			MD5: fixture.item.MD5, SHA256: fixture.item.SHA256, Filename: fixture.item.Filename,
			Format: fixture.item.Format, Size: fixture.item.Size, Caption: "another concurrent revision",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	state.Revision = hashText(changedManifest)
	state.Manifest = changedManifest
	stateBytes, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, filepath.Join(home, ".sticker", "packs", "curated.json"), stateBytes)
	plan, err := makeInstallPlan(context.Background(), prepared.prepared)
	if err != nil {
		t.Fatal(err)
	}
	_, err = publishUpdate(context.Background(), prepared, map[string]stagedImage{}, UpdateResult{
		Source: prepared.prepared.source.Canonical, Target: prepared.prepared.root.Root,
		Pack: prepared.prepared.snapshot.pack, Revision: plan.Revision,
	}, nil)
	assertPackError(t, err, "conflict", "state_changed")
	if _, currentInstalled, stateErr := readInstalledState(home, "curated"); stateErr != nil || !currentInstalled {
		t.Fatalf("concurrent state was lost: %v, %v", stateErr, currentInstalled)
	}
}

func writeUpdatedPackFixture(t *testing.T, fixture installFixture, items []library.Item, contents [][]byte) []byte {
	t.Helper()
	manifestBytes, err := json.Marshal(library.Manifest{SchemaVersion: 1, Collection: "curated", Items: items})
	if err != nil {
		t.Fatal(err)
	}
	revision := hashText(manifestBytes)
	directoryBytes, err := json.Marshal(directory{SchemaVersion: 1, Packs: []entry{{
		ID: "curated", Name: "Curated", Description: "Updated selection", Manifest: "packs/curated.json", ManifestSHA256: revision, Count: len(items), Size: manifestSizeForItems(items),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, filepath.Join(fixture.root, "packs.json"), directoryBytes)
	writeFixtureFile(t, filepath.Join(fixture.root, "packs", "curated.json"), manifestBytes)
	for index, content := range contents {
		if content == nil {
			continue
		}
		writeFixtureFile(t, filepath.Join(fixture.root, filepath.FromSlash(items[index].Filename)), content)
	}
	return manifestBytes
}

func manifestSizeForItems(items []library.Item) int64 {
	var size int64
	for _, item := range items {
		size += item.Size
	}
	return size
}
