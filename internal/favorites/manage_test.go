package favorites

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"github.com/9Ashwin/sticker-cli/internal/library"
)

func addTestFavorite(t *testing.T, home, name, caption string) library.Item {
	t.Helper()
	path := filepath.Join(t.TempDir(), name+".gif")
	if err := os.WriteFile(path, []byte("GIF89a-"+name), 0o600); err != nil {
		t.Fatal(err)
	}
	var captionPtr *string
	if caption != "" {
		captionPtr = &caption
	}
	result, err := Execute(context.Background(), Options{Home: home, Path: path, Caption: captionPtr})
	if err != nil {
		t.Fatal(err)
	}
	return library.Item{MD5: result.Item.MD5, SHA256: result.Item.SHA256, Filename: result.Item.Filename, Format: result.Item.Format, Size: result.Item.Size, Caption: result.Item.Caption}
}

func TestListPaginatesPersonalManifestAndSortsCaptions(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	addTestFavorite(t, home, "one", "zebra")
	addTestFavorite(t, home, "two", "Alpha")
	addTestFavorite(t, home, "three", "beta")

	page, err := List(context.Background(), ListOptions{Home: home, Sort: "caption", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || len(page.Items) != 2 || !page.HasMore || page.NextOffset != 2 {
		t.Fatalf("unexpected first page: %+v", page)
	}
	if page.Items[0].Caption != "Alpha" || page.Items[1].Caption != "beta" {
		t.Fatalf("caption ordering = %q, %q", page.Items[0].Caption, page.Items[1].Caption)
	}
	for _, item := range page.Items {
		if !item.Favorite || !filepath.IsAbs(item.Path) || item.Packs == nil {
			t.Fatalf("incomplete list item: %+v", item)
		}
	}

	page, err = List(context.Background(), ListOptions{Home: home, Sort: "caption", Limit: 2, Offset: 2})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || len(page.Items) != 1 || page.HasMore || page.NextOffset != 3 || page.Items[0].Caption != "zebra" {
		t.Fatalf("unexpected second page: %+v", page)
	}

	page, err = List(context.Background(), ListOptions{Home: home, Sort: "md5", Limit: 2, Offset: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || len(page.Items) != 0 || page.HasMore || page.NextOffset != 10 {
		t.Fatalf("unexpected empty page: %+v", page)
	}
}

func TestListCollectionSortsStableKeysAndManifestFallback(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	first := addTestFavorite(t, home, "sort-first", "same")
	second := addTestFavorite(t, home, "sort-second", "same")
	third := addTestFavorite(t, home, "sort-third", "other")
	root, err := library.New(home)
	if err != nil {
		t.Fatal(err)
	}
	state := Collections{SchemaVersion: 1, Collections: []Collection{
		{ID: DefaultCollectionID, Name: DefaultCollectionName, Position: 0, Items: []CollectionItem{
			{ID: first.MD5, Position: 0}, {ID: second.MD5, Position: 1}, {ID: third.MD5, Position: 2},
		}},
		{ID: "work", Name: "Work", Position: 1, Items: []CollectionItem{
			{ID: first.MD5, Position: 2, AddedAt: "2026-01-02T03:04:05Z"},
			{ID: second.MD5, Position: 1, AddedAt: "2026-01-01T03:04:05Z"},
			{ID: third.MD5, Position: 0},
		}},
	}}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.WriteRelativeAtomic(context.Background(), CollectionsRelativePath, data); err != nil {
		t.Fatal(err)
	}

	listIDs := func(order string) []string {
		t.Helper()
		result, err := List(context.Background(), ListOptions{Home: home, Collection: "work", Sort: order, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		ids := make([]string, 0, len(result.Items))
		for _, item := range result.Items {
			ids = append(ids, item.ID)
		}
		return ids
	}
	if got, want := listIDs("manual"), []string{third.MD5, second.MD5, first.MD5}; !equalStrings(got, want) {
		t.Fatalf("manual order = %v, want %v", got, want)
	}
	if got, want := listIDs("added"), []string{second.MD5, first.MD5, third.MD5}; !equalStrings(got, want) {
		t.Fatalf("added order = %v, want %v", got, want)
	}
	captionIDs := []string{first.MD5, second.MD5}
	sort.Strings(captionIDs)
	if got, want := listIDs("caption"), append([]string{third.MD5}, captionIDs...); !equalStrings(got, want) {
		t.Fatalf("caption order = %v, want %v", got, want)
	}
	md5IDs := []string{first.MD5, second.MD5, third.MD5}
	sort.Strings(md5IDs)
	if got := listIDs("md5"); !equalStrings(got, md5IDs) {
		t.Fatalf("md5 order = %v, want %v", got, md5IDs)
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func TestDescribeSupportsDryRunAndExplicitEmptyCaption(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	item := addTestFavorite(t, home, "describe", "old")

	dryRun, err := Describe(context.Background(), DescribeOptions{Home: home, ID: item.MD5, Caption: "new", DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !dryRun.Updated || !dryRun.DryRun || dryRun.Item.Caption != "new" {
		t.Fatalf("unexpected dry-run result: %+v", dryRun)
	}
	root, err := library.New(home)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := root.ReadManifest(context.Background())
	if err != nil || manifest.Items[0].Caption != "old" {
		t.Fatalf("dry-run changed manifest: %+v %v", manifest, err)
	}

	updated, err := Describe(context.Background(), DescribeOptions{Home: home, ID: item.MD5, Caption: "new"})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Updated || updated.DryRun || updated.Item.Caption != "new" {
		t.Fatalf("unexpected update result: %+v", updated)
	}
	cleared, err := Describe(context.Background(), DescribeOptions{Home: home, ID: item.MD5, Caption: ""})
	if err != nil {
		t.Fatal(err)
	}
	if !cleared.Updated || cleared.Item.Caption != "" {
		t.Fatalf("unexpected clear result: %+v", cleared)
	}
}

func TestRemoveIsIdempotentAndRetainsOriginal(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	item := addTestFavorite(t, home, "remove", "keep")
	root, err := library.New(home)
	if err != nil {
		t.Fatal(err)
	}
	path, err := root.ItemPath(item)
	if err != nil {
		t.Fatal(err)
	}

	dryRun, err := Remove(context.Background(), RemoveOptions{Home: home, IDs: []string{item.MD5}, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if dryRun.Removed != 1 || dryRun.RetainedOriginal != 1 || !dryRun.DryRun || dryRun.Committed {
		t.Fatalf("unexpected remove dry-run: %+v", dryRun)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("dry-run removed original: %v", err)
	}

	removed, err := Remove(context.Background(), RemoveOptions{Home: home, IDs: []string{item.MD5, item.MD5}})
	if err != nil {
		t.Fatal(err)
	}
	if removed.Removed != 1 || removed.RetainedOriginal != 1 || !removed.Committed {
		t.Fatalf("unexpected remove result: %+v", removed)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("remove deleted original: %v", err)
	}
	second, err := Remove(context.Background(), RemoveOptions{Home: home, IDs: []string{item.MD5}})
	if err != nil {
		t.Fatal(err)
	}
	if second.Removed != 0 || second.RetainedOriginal != 0 || second.Committed {
		t.Fatalf("repeat remove is not idempotent: %+v", second)
	}
	manifest, err := root.ReadManifest(context.Background())
	if err != nil || len(manifest.Items) != 0 {
		t.Fatalf("removed item remains in manifest: %+v %v", manifest, err)
	}
}

func TestRemoveClearsCollectionMembership(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	item := addTestFavorite(t, home, "remove-from-collection", "playful scene")
	if _, err := CreateCollection(context.Background(), CollectionCreateOptions{Home: home, Name: "work"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Organize(context.Background(), OrganizeOptions{
		Home:       home,
		Collection: DefaultCollectionID,
		IDs:        []string{item.MD5},
		MoveTo:     "work",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := Remove(context.Background(), RemoveOptions{Home: home, IDs: []string{item.MD5}}); err != nil {
		t.Fatal(err)
	}
	collections, err := ListCollections(context.Background(), CollectionListOptions{Home: home})
	if err != nil {
		t.Fatal(err)
	}
	for _, collection := range collections.Collections {
		if len(collection.Items) != 0 {
			t.Fatalf("removed item remains in collection %q: %+v", collection.ID, collection.Items)
		}
	}
	filtered, err := List(context.Background(), ListOptions{Home: home, Collection: "work", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if filtered.Total != 0 || len(filtered.Items) != 0 {
		t.Fatalf("removed item remains in filtered favorites: %+v", filtered)
	}
}

func TestConcurrentDescribeUpdatesKeepBothCaptions(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	first := addTestFavorite(t, home, "first", "one")
	second := addTestFavorite(t, home, "second", "two")

	var group sync.WaitGroup
	group.Add(2)
	var firstErr, secondErr error
	go func() {
		defer group.Done()
		_, firstErr = Describe(context.Background(), DescribeOptions{Home: home, ID: first.MD5, Caption: "updated one"})
	}()
	go func() {
		defer group.Done()
		_, secondErr = Describe(context.Background(), DescribeOptions{Home: home, ID: second.MD5, Caption: "updated two"})
	}()
	group.Wait()
	if firstErr != nil || secondErr != nil {
		t.Fatalf("concurrent describes failed: %v / %v", firstErr, secondErr)
	}
	root, err := library.New(home)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := root.ReadManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]string, len(manifest.Items))
	for _, item := range manifest.Items {
		got[item.MD5] = item.Caption
	}
	if got[first.MD5] != "updated one" || got[second.MD5] != "updated two" {
		t.Fatalf("concurrent captions lost: %v", got)
	}
}
