package packs

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/9Ashwin/sticker-cli/internal/library"
)

func TestRemoveRetainsOriginalAndPersonalCaption(t *testing.T) {
	fixture := newInstallFixture(t)
	home := filepath.Join(t.TempDir(), "library")
	if _, err := Install(context.Background(), InstallOptions{Home: home, Source: fixture.root, PackID: "curated"}); err != nil {
		t.Fatal(err)
	}
	root, err := library.New(home)
	if err != nil {
		t.Fatal(err)
	}
	personal := library.Manifest{SchemaVersion: 1, Collection: "personal", Items: []library.Item{{
		MD5: fixture.item.MD5, SHA256: fixture.item.SHA256, Filename: fixture.item.Filename,
		Format: fixture.item.Format, Size: fixture.item.Size, Caption: "my favorite",
	}}}
	if err := root.WriteManifest(context.Background(), personal); err != nil {
		t.Fatal(err)
	}

	planned, err := Remove(context.Background(), RemoveOptions{Home: home, PackID: "curated", DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !planned.Removed || planned.RetainedBytes != fixture.item.Size || planned.Committed || !planned.DryRun {
		t.Fatalf("unexpected dry-run result: %+v", planned)
	}
	if _, installed, err := readInstalledState(home, "curated"); err != nil || !installed {
		t.Fatalf("dry-run changed installed state: %v, %v", err, installed)
	}

	result, err := Remove(context.Background(), RemoveOptions{Home: home, PackID: "curated"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Removed || result.RetainedBytes != fixture.item.Size || !result.Committed || result.DryRun {
		t.Fatalf("unexpected remove result: %+v", result)
	}
	if _, installed, err := readInstalledState(home, "curated"); err != nil || installed {
		t.Fatalf("installed state remains after remove: %v, %v", err, installed)
	}
	if err := root.VerifyItem(context.Background(), fixture.item); err != nil {
		t.Fatalf("remove deleted or damaged the original: %v", err)
	}
	current, err := root.ReadManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(current.Items) != 1 || current.Items[0].Caption != "my favorite" {
		t.Fatalf("remove changed the personal caption: %+v", current)
	}

	repeated, err := Remove(context.Background(), RemoveOptions{Home: home, PackID: "curated"})
	if err != nil {
		t.Fatal(err)
	}
	if repeated.Removed || repeated.RetainedBytes != 0 || repeated.Committed || repeated.DryRun {
		t.Fatalf("unexpected repeated remove result: %+v", repeated)
	}
}

func TestRemoveKeepsSharedImageAfterBothPackStatesAreRemoved(t *testing.T) {
	fixture := newInstallFixture(t)
	home := filepath.Join(t.TempDir(), "library")
	if _, err := Install(context.Background(), InstallOptions{Home: home, Source: fixture.root, PackID: "curated"}); err != nil {
		t.Fatal(err)
	}
	root, err := library.New(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.WriteManifest(context.Background(), library.Manifest{SchemaVersion: 1, Collection: "personal", Items: []library.Item{{
		MD5: fixture.item.MD5, SHA256: fixture.item.SHA256, Filename: fixture.item.Filename,
		Format: fixture.item.Format, Size: fixture.item.Size, Caption: "playful favorite",
	}}}); err != nil {
		t.Fatal(err)
	}
	var sourceDirectory directory
	if err := json.Unmarshal(fixture.directory, &sourceDirectory); err != nil {
		t.Fatal(err)
	}
	sourceDirectory.Packs = append(sourceDirectory.Packs, entry{
		ID: "all", Name: "All", Description: "Everything", Manifest: "manifest.json",
		ManifestSHA256: fixture.revision, Count: 1, Size: fixture.item.Size,
	})
	directoryBytes, err := json.Marshal(sourceDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.root, "packs.json"), directoryBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.root, "manifest.json"), fixture.manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(context.Background(), InstallOptions{Home: home, Source: fixture.root, PackID: "all"}); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(home, ".sticker", "packs")

	if result, err := Remove(context.Background(), RemoveOptions{Home: home, PackID: "curated"}); err != nil || !result.Removed {
		t.Fatalf("curated remove failed: %+v, %v", result, err)
	}
	if result, err := Remove(context.Background(), RemoveOptions{Home: home, PackID: "all"}); err != nil || !result.Removed {
		t.Fatalf("all remove failed: %+v, %v", result, err)
	}
	if err := root.VerifyItem(context.Background(), fixture.item); err != nil {
		t.Fatalf("shared original is not readable after removing both packs: %v", err)
	}
	manifest, err := root.ReadManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Items) != 1 || manifest.Items[0].Caption != "playful favorite" {
		t.Fatalf("favorite changed after removing both packs: %+v", manifest)
	}
	for _, id := range []string{"curated", "all"} {
		if _, err := os.Stat(filepath.Join(stateDir, id+".json")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("state %s remains after remove: %v", id, err)
		}
	}
}

func TestRemoveRequiresValidPackID(t *testing.T) {
	for _, id := range []string{"", "../escape", "A"} {
		_, err := Remove(context.Background(), RemoveOptions{Home: t.TempDir(), PackID: id})
		assertPackError(t, err, "validation", "invalid_argument")
	}
}

func TestRemoveClearsCorruptStateAndRetainsOriginal(t *testing.T) {
	fixture := newInstallFixture(t)
	home := filepath.Join(t.TempDir(), "library")
	if _, err := Install(context.Background(), InstallOptions{Home: home, Source: fixture.root, PackID: "curated"}); err != nil {
		t.Fatal(err)
	}

	statePath := filepath.Join(home, ".sticker", "packs", "curated.json")
	stateBytes, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state installedState
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatal(err)
	}
	state.Revision = strings.Repeat("0", 64)
	corruptBytes, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, statePath, corruptBytes)

	planned, err := Remove(context.Background(), RemoveOptions{Home: home, PackID: "curated", DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !planned.Removed || !planned.StateCorrupt || planned.RetainedBytes != 0 || planned.Committed || !planned.DryRun {
		t.Fatalf("unexpected corrupt-state dry-run: %+v", planned)
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("dry-run removed corrupt state: %v", err)
	}

	result, err := Remove(context.Background(), RemoveOptions{Home: home, PackID: "curated"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Removed || !result.StateCorrupt || result.RetainedBytes != 0 || !result.Committed || result.DryRun {
		t.Fatalf("unexpected corrupt-state removal: %+v", result)
	}
	if _, err := os.Stat(statePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("corrupt state remains after removal: %v", err)
	}
	root, err := library.New(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.VerifyItem(context.Background(), fixture.item); err != nil {
		t.Fatalf("remove damaged the original: %v", err)
	}
}
