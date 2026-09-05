package favorites

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/9Ashwin/sticker-cli/internal/library"
)

func TestCollectionsCRUDAndMembershipPreserveManifestAndOriginals(t *testing.T) {
	parent := t.TempDir()
	home := filepath.Join(parent, "home")
	first := addTestFavorite(t, home, "first", "first scene")
	second := addTestFavorite(t, home, "second", "second scene")
	root, err := library.New(home)
	if err != nil {
		t.Fatal(err)
	}
	beforeManifest, err := os.ReadFile(filepath.Join(home, library.ManifestName))
	if err != nil {
		t.Fatal(err)
	}
	firstPath, err := root.ItemPath(library.Item{MD5: first.MD5, SHA256: first.SHA256, Filename: first.Filename, Format: first.Format, Size: first.Size, Caption: first.Caption})
	if err != nil {
		t.Fatal(err)
	}

	listed, err := ListCollections(context.Background(), CollectionListOptions{Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Collections) != 1 || listed.Collections[0].ID != DefaultCollectionID || len(listed.Collections[0].Items) != 2 {
		t.Fatalf("unexpected legacy collections: %+v", listed)
	}

	created, err := CreateCollection(context.Background(), CollectionCreateOptions{Home: home, Name: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if !created.Changed || created.Collection.ID != "work" || created.Collection.Name != "work" {
		t.Fatalf("unexpected created collection: %+v", created)
	}
	if _, err := CreateCollection(context.Background(), CollectionCreateOptions{Home: home, Name: "work", DryRun: true}); !isLibraryError(err, "conflict", "collection_exists") {
		t.Fatalf("duplicate collection error = %v", err)
	}

	renamed, err := RenameCollection(context.Background(), CollectionRenameOptions{Home: home, ID: "work", Name: "工作"})
	if err != nil {
		t.Fatal(err)
	}
	if !renamed.Changed || renamed.Collection.Name != "工作" {
		t.Fatalf("unexpected renamed collection: %+v", renamed)
	}
	if _, err := RenameCollection(context.Background(), CollectionRenameOptions{Home: home, ID: DefaultCollectionID, Name: "other"}); !isLibraryError(err, "validation", "invalid_argument") {
		t.Fatalf("default rename error = %v", err)
	}

	moved, err := Organize(context.Background(), OrganizeOptions{Home: home, Collection: DefaultCollectionID, IDs: []string{first.MD5}, MoveTo: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if moved.Moved != 1 || moved.Removed != 1 || !moved.Committed {
		t.Fatalf("unexpected move result: %+v", moved)
	}
	workItems, err := List(context.Background(), ListOptions{Home: home, Collection: "work", Sort: "manual", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if workItems.Total != 1 || workItems.Items[0].ID != first.MD5 || workItems.Items[0].Path != firstPath {
		t.Fatalf("unexpected work members: %+v", workItems)
	}

	removed, err := Organize(context.Background(), OrganizeOptions{Home: home, Collection: "work", IDs: []string{first.MD5}, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if removed.Removed != 1 || removed.Committed || !removed.DryRun {
		t.Fatalf("unexpected relationship dry-run: %+v", removed)
	}
	removed, err = Organize(context.Background(), OrganizeOptions{Home: home, Collection: "work", IDs: []string{first.MD5}})
	if err != nil {
		t.Fatal(err)
	}
	if removed.Removed != 1 || !removed.Committed {
		t.Fatalf("unexpected relationship remove: %+v", removed)
	}

	deleted, err := RemoveCollection(context.Background(), CollectionRemoveOptions{Home: home, ID: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if !deleted.Removed || deleted.Moved != 0 {
		t.Fatalf("unexpected empty collection removal: %+v", deleted)
	}
	if _, err := RemoveCollection(context.Background(), CollectionRemoveOptions{Home: home, ID: DefaultCollectionID}); !isLibraryError(err, "validation", "invalid_argument") {
		t.Fatalf("default remove error = %v", err)
	}

	afterManifest, err := os.ReadFile(filepath.Join(home, library.ManifestName))
	if err != nil {
		t.Fatal(err)
	}
	if string(afterManifest) != string(beforeManifest) {
		t.Fatal("collection operations changed the standard manifest")
	}
	if err := root.VerifyItem(context.Background(), library.Item{MD5: first.MD5, SHA256: first.SHA256, Filename: first.Filename, Format: first.Format, Size: first.Size}); err != nil {
		t.Fatalf("collection operations damaged original: %v", err)
	}
	_ = second
}

func TestImportCreatesDefaultCollectionAndAcceptsValidatedExtension(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "source")
	firstData := []byte("GIF89a extension first")
	secondData := []byte("GIF89a extension second")
	first := itemForData(firstData, "first")
	second := itemForData(secondData, "second")
	writeItem(t, source, first, firstData)
	writeItem(t, source, second, secondData)
	writeManifest(t, source, "shared", first, second)

	withoutExtensionHome := filepath.Join(parent, "without-extension")
	result, err := Import(context.Background(), ImportOptions{Home: withoutExtensionHome, Source: source})
	if err != nil || !result.Committed {
		t.Fatalf("v1 import failed: %+v %v", result, err)
	}
	withoutExtension, err := ListCollections(context.Background(), CollectionListOptions{Home: withoutExtensionHome})
	if err != nil {
		t.Fatal(err)
	}
	if len(withoutExtension.Collections) != 1 || len(withoutExtension.Collections[0].Items) != 2 {
		t.Fatalf("v1 import did not populate default collection: %+v", withoutExtension)
	}
	if _, err := os.Stat(filepath.Join(withoutExtensionHome, CollectionsRelativePath)); err != nil {
		t.Fatalf("v1 import did not persist collection metadata: %v", err)
	}

	extension := Collections{SchemaVersion: 1, Collections: []Collection{
		{ID: DefaultCollectionID, Name: DefaultCollectionName, Position: 0, Items: []CollectionItem{{ID: second.MD5, Position: 0}}},
		{ID: "work", Name: "Work", Position: 1, Items: []CollectionItem{{ID: first.MD5, Position: 0}}},
	}}
	extBytes, err := json.MarshalIndent(extension, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, CollectionsExtensionName), append(extBytes, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	withExtensionHome := filepath.Join(parent, "with-extension")
	if _, err := Import(context.Background(), ImportOptions{Home: withExtensionHome, Source: source}); err != nil {
		t.Fatal(err)
	}
	withExtension, err := ListCollections(context.Background(), CollectionListOptions{Home: withExtensionHome})
	if err != nil {
		t.Fatal(err)
	}
	if len(withExtension.Collections) != 2 || len(withExtension.Collections[1].Items) != 1 || withExtension.Collections[1].Items[0].ID != first.MD5 {
		t.Fatalf("extension memberships were not imported: %+v", withExtension)
	}
}

func TestCorruptCollectionsMetadataDoesNotHideManifest(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	item := addTestFavorite(t, home, "corrupt", "playful scene")
	metadataPath := filepath.Join(home, CollectionsRelativePath)
	if err := os.WriteFile(metadataPath, []byte(`{"schema_version":1,"collections":[`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := ListCollections(context.Background(), CollectionListOptions{Home: home})
	if !isLibraryError(err, "integrity", "invalid_collection") {
		t.Fatalf("corrupt metadata error = %v", err)
	}
	root, err := library.New(home)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := root.ReadManifest(context.Background())
	if err != nil || len(manifest.Items) != 1 || manifest.Items[0].MD5 != item.MD5 {
		t.Fatalf("manifest became unreadable after metadata corruption: %+v %v", manifest, err)
	}
}

func isLibraryError(err error, kind, subtype string) bool {
	var coded *library.Error
	return errors.As(err, &coded) && coded.Kind == kind && coded.Subtype == subtype
}
