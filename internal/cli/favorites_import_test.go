package cli

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/9Ashwin/sticker-cli/internal/library"
)

func TestFavoriteImportCLI(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "source")
	home := filepath.Join(parent, "home")
	data := []byte("GIF89a cli import")
	item := cliImportItem(data, "from source")
	if err := os.MkdirAll(filepath.Join(source, library.EmoticonsDirectory), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, filepath.FromSlash(item.Filename)), data, 0o600); err != nil {
		t.Fatal(err)
	}
	sourceLibrary, err := library.New(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := sourceLibrary.WriteManifest(context.Background(), library.Manifest{SchemaVersion: 1, Collection: "shared", Items: []library.Item{item}}); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"--home", home, "favorites", "import", source}, &out, &errOut, "dev", "test"); code != 0 {
		t.Fatalf("import exit %d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	var response struct {
		OK   bool `json:"ok"`
		Data struct {
			Added     int  `json:"added"`
			Committed bool `json:"committed"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.OK || response.Data.Added != 1 || !response.Data.Committed || errOut.Len() != 0 {
		t.Fatalf("unexpected import response: stdout=%s stderr=%s", out.String(), errOut.String())
	}
}

func TestFavoriteImportCLIDryRun(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "source")
	home := filepath.Join(parent, "home")
	data := []byte("GIF89a cli import dry run")
	item := cliImportItem(data, "dry run")
	if err := os.MkdirAll(filepath.Join(source, library.EmoticonsDirectory), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, filepath.FromSlash(item.Filename)), data, 0o600); err != nil {
		t.Fatal(err)
	}
	sourceLibrary, err := library.New(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := sourceLibrary.WriteManifest(context.Background(), library.Manifest{SchemaVersion: 1, Collection: "shared", Items: []library.Item{item}}); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"--home", home, "favorites", "import", source, "--dry-run"}, &out, &errOut, "dev", "test"); code != 0 {
		t.Fatalf("dry-run exit %d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	if !bytes.Contains(out.Bytes(), []byte(`"dry_run":true`)) || !bytes.Contains(out.Bytes(), []byte(`"committed":false`)) {
		t.Fatalf("unexpected dry-run response: %s", out.String())
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("dry-run created home: %v", err)
	}
}

func TestFavoriteImportSchemaDeclaresFlags(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"schema", "favorites", "import"}, &out, &errOut, "dev", "test"); code != 0 {
		t.Fatalf("schema exit %d: %s", code, errOut.String())
	}
	if !bytes.Contains(out.Bytes(), []byte(`"name":"--overwrite-captions"`)) || !bytes.Contains(out.Bytes(), []byte(`"name":"--dry-run"`)) || !bytes.Contains(out.Bytes(), []byte(`"committed"`)) {
		t.Fatalf("favorite import schema is incomplete: %s", out.String())
	}
}

func cliImportItem(data []byte, caption string) library.Item {
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
