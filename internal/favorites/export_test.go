package favorites

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/9Ashwin/sticker-cli/internal/library"
	"github.com/9Ashwin/sticker-cli/internal/packs"
)

func TestExportProducesInstallablePackAndRoundTrips(t *testing.T) {
	parent := t.TempDir()
	home := filepath.Join(parent, "home")
	firstPath := filepath.Join(parent, "first.gif")
	secondPath := filepath.Join(parent, "second.gif")
	firstData := []byte("GIF89a export first")
	secondData := []byte("GIF89a export second")
	writeFile(t, firstPath, firstData)
	writeFile(t, secondPath, secondData)
	first := addFavoriteFromPath(t, home, firstPath, "first scene")
	second := addFavoriteFromPath(t, home, secondPath, "second scene")

	destination := filepath.Join(parent, "export")
	result, err := Export(context.Background(), ExportOptions{Home: home, Destination: destination})
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != destination || result.Count != 2 || result.Size != first.Size+second.Size || result.DryRun {
		t.Fatalf("unexpected export result: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(destination, ".sticker")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("export unexpectedly contains private state directory: %v", err)
	}

	exportedRoot, err := library.New(destination)
	if err != nil {
		t.Fatal(err)
	}
	exported, err := exportedRoot.ReadManifestRequired(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(exported.Items) != 2 || exported.Items[0].Caption != first.Caption || exported.Items[1].Caption != second.Caption {
		t.Fatalf("exported manifest lost entries or captions: %+v", exported)
	}
	if err := exportedRoot.Validate(context.Background(), exported); err != nil {
		t.Fatalf("exported pack is not valid: %v", err)
	}

	var directory struct {
		SchemaVersion int `json:"schema_version"`
		Packs         []struct {
			ID             string `json:"id"`
			Manifest       string `json:"manifest"`
			ManifestSHA256 string `json:"manifest_sha256"`
			Count          int    `json:"count"`
			Size           int64  `json:"size"`
		} `json:"packs"`
	}
	directoryBytes, err := os.ReadFile(filepath.Join(destination, "packs.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(directoryBytes, &directory); err != nil {
		t.Fatal(err)
	}
	if directory.SchemaVersion != 1 || len(directory.Packs) != 1 || directory.Packs[0].ID != exportPackID || directory.Packs[0].Manifest != library.ManifestName || directory.Packs[0].Count != 2 || directory.Packs[0].Size != result.Size {
		t.Fatalf("unexpected exported pack directory: %+v", directory)
	}

	catalog, err := packs.Discover(context.Background(), packs.Options{Home: filepath.Join(parent, "catalog-home"), Source: destination})
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Items) != 1 || catalog.Items[0].ID != exportPackID || catalog.Items[0].Count != result.Count || catalog.Items[0].Size != result.Size {
		t.Fatalf("export is not discoverable as a pack: %+v", catalog.Items)
	}
	installed, err := packs.Install(context.Background(), packs.InstallOptions{Home: filepath.Join(parent, "installed"), Source: destination, PackID: exportPackID})
	if err != nil {
		t.Fatal(err)
	}
	if installed.Pack.ID != exportPackID || installed.Added != 2 || installed.DownloadBytes != result.Size {
		t.Fatalf("export is not installable: %+v", installed)
	}

	importHome := filepath.Join(parent, "imported")
	imported, err := Import(context.Background(), ImportOptions{Home: importHome, Source: destination})
	if err != nil {
		t.Fatal(err)
	}
	if imported.Added != 2 || !imported.Committed {
		t.Fatalf("unexpected round-trip import result: %+v", imported)
	}
	importedRoot, err := library.New(importHome)
	if err != nil {
		t.Fatal(err)
	}
	got, err := importedRoot.ReadManifestRequired(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != len(exported.Items) {
		t.Fatalf("round-trip item count = %d, want %d", len(got.Items), len(exported.Items))
	}
	for index := range exported.Items {
		if got.Items[index] != exported.Items[index] {
			t.Fatalf("round-trip item %d = %+v, want %+v", index, got.Items[index], exported.Items[index])
		}
	}
	if err := importedRoot.Validate(context.Background(), got); err != nil {
		t.Fatalf("round-trip library is not valid: %v", err)
	}
}

func TestExportDryRunAndDestinationConflictDoNotWrite(t *testing.T) {
	parent := t.TempDir()
	home := filepath.Join(parent, "home")
	path := filepath.Join(parent, "favorite.gif")
	writeFile(t, path, []byte("GIF89a export dry run"))
	addFavoriteFromPath(t, home, path, "dry run")

	dryDestination := filepath.Join(parent, "dry-run")
	result, err := Export(context.Background(), ExportOptions{Home: home, Destination: dryDestination, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.DryRun || result.Count != 1 || result.Path != dryDestination {
		t.Fatalf("unexpected dry-run result: %+v", result)
	}
	if _, err := os.Stat(dryDestination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run wrote destination: %v", err)
	}

	existing := filepath.Join(parent, "existing")
	writeFile(t, filepath.Join(existing, "keep.txt"), []byte("keep"))
	_, err = Export(context.Background(), ExportOptions{Home: home, Destination: existing})
	var coded *library.Error
	if !errors.As(err, &coded) || coded.Kind != "conflict" || coded.Subtype != "destination_exists" {
		t.Fatalf("expected destination conflict, got %T %v", err, err)
	}
	if got, readErr := os.ReadFile(filepath.Join(existing, "keep.txt")); readErr != nil || string(got) != "keep" {
		t.Fatalf("existing destination changed: %q %v", got, readErr)
	}
	if leftovers, readErr := filepath.Glob(filepath.Join(parent, ".sticker-export-*")); readErr != nil || len(leftovers) != 0 {
		t.Fatalf("export staging was not cleaned: %v %v", leftovers, readErr)
	}
}

func TestExportRoundTripsCollectionOrderAndMembership(t *testing.T) {
	parent := t.TempDir()
	home := filepath.Join(parent, "home")
	firstPath := filepath.Join(parent, "first.gif")
	secondPath := filepath.Join(parent, "second.gif")
	thirdPath := filepath.Join(parent, "third.gif")
	writeFile(t, firstPath, []byte("GIF89a collection first"))
	writeFile(t, secondPath, []byte("GIF89a collection second"))
	writeFile(t, thirdPath, []byte("GIF89a collection third"))
	first := addFavoriteFromPath(t, home, firstPath, "first")
	second := addFavoriteFromPath(t, home, secondPath, "second")
	third := addFavoriteFromPath(t, home, thirdPath, "third")
	if _, err := CreateCollection(context.Background(), CollectionCreateOptions{Home: home, Name: "work"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Organize(context.Background(), OrganizeOptions{
		Home: home, Collection: DefaultCollectionID, IDs: []string{first.MD5, second.MD5}, MoveTo: "work",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := Organize(context.Background(), OrganizeOptions{
		Home: home, Collection: "work", Order: []string{second.MD5, first.MD5},
	}); err != nil {
		t.Fatal(err)
	}

	destination := filepath.Join(parent, "export")
	if _, err := Export(context.Background(), ExportOptions{Home: home, Destination: destination}); err != nil {
		t.Fatal(err)
	}
	extensionBytes, err := os.ReadFile(filepath.Join(destination, CollectionsExtensionName))
	if err != nil {
		t.Fatal(err)
	}
	var extension Collections
	if err := json.Unmarshal(extensionBytes, &extension); err != nil {
		t.Fatal(err)
	}
	if len(extension.Collections) != 2 || extension.Collections[0].ID != DefaultCollectionID || extension.Collections[0].Items[0].ID != third.MD5 || extension.Collections[1].ID != "work" {
		t.Fatalf("unexpected exported collections: %+v", extension)
	}
	if got := extension.Collections[1].Items; len(got) != 2 || got[0].ID != second.MD5 || got[1].ID != first.MD5 || got[0].Position != 0 || got[1].Position != 1 {
		t.Fatalf("unexpected exported work order: %+v", got)
	}

	importHome := filepath.Join(parent, "imported")
	if _, err := Import(context.Background(), ImportOptions{Home: importHome, Source: destination}); err != nil {
		t.Fatal(err)
	}
	collections, err := ListCollections(context.Background(), CollectionListOptions{Home: importHome})
	if err != nil {
		t.Fatal(err)
	}
	if len(collections.Collections) != 2 || len(collections.Collections[0].Items) != 1 || collections.Collections[0].Items[0].ID != third.MD5 {
		t.Fatalf("round-trip default collection = %+v", collections.Collections)
	}
	work, ok := findCollection(Collections{Collections: collections.Collections}, "work")
	if !ok || len(work.Items) != 2 || work.Items[0].ID != second.MD5 || work.Items[1].ID != first.MD5 {
		t.Fatalf("round-trip work collection = %+v", collections.Collections)
	}
}

func TestExportMissingImageLeavesNoStagingDirectory(t *testing.T) {
	parent := t.TempDir()
	home := filepath.Join(parent, "home")
	data := []byte("GIF89a export missing")
	item := itemForData(data, "missing")
	writeManifest(t, home, "personal", item)
	_, err := Export(context.Background(), ExportOptions{Home: home, Destination: filepath.Join(parent, "export")})
	if err == nil {
		t.Fatal("export unexpectedly succeeded with a missing image")
	}
	if leftovers, readErr := filepath.Glob(filepath.Join(parent, ".sticker-export-*")); readErr != nil || len(leftovers) != 0 {
		t.Fatalf("failed export left staging directories: %v %v", leftovers, readErr)
	}
}

func addFavoriteFromPath(t *testing.T, home, path, caption string) library.Item {
	t.Helper()
	result, err := Execute(context.Background(), Options{Home: home, Path: path, Caption: &caption})
	if err != nil {
		t.Fatal(err)
	}
	return library.Item{
		MD5: result.Item.MD5, SHA256: result.Item.SHA256, Filename: result.Item.Filename,
		Format: result.Item.Format, Size: result.Item.Size, Caption: result.Item.Caption,
	}
}
