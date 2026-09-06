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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/9Ashwin/sticker-cli/internal/library"
)

func TestPackRemoveCLIIsIdempotentAndRetainsBytes(t *testing.T) {
	home := t.TempDir()
	content := []byte("GIF89a pack remove cli")
	md5Sum := md5.Sum(content)
	shaSum := sha256.Sum256(content)
	id := hex.EncodeToString(md5Sum[:])
	item := library.Item{MD5: id, SHA256: hex.EncodeToString(shaSum[:]), Filename: "emoticons/" + id + ".gif", Format: "gif", Size: int64(len(content)), Caption: "pack caption"}
	manifest := library.Manifest{SchemaVersion: 1, Collection: "curated", Items: []library.Item{item}}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	state := struct {
		SchemaVersion int             `json:"schema_version"`
		ID            string          `json:"id"`
		Source        string          `json:"source"`
		Revision      string          `json:"revision"`
		InstalledAt   time.Time       `json:"installed_at"`
		Manifest      json.RawMessage `json:"manifest"`
	}{1, "curated", "/tmp/source", hashBytes(manifestBytes), time.Now().UTC(), manifestBytes}
	stateBytes, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(home, ".sticker", "packs")
	if err := os.MkdirAll(filepath.Dir(filepath.Join(home, filepath.FromSlash(item.Filename))), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, filepath.FromSlash(item.Filename)), content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "curated.json"), stateBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"--home", home, "packs", "remove", "curated", "--dry-run"}, &out, &errOut, "dev", "test"); code != 0 {
		t.Fatalf("dry-run exit %d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), `"removed":true`) || !strings.Contains(out.String(), `"retained_bytes":`+strconv.FormatInt(int64(len(content)), 10)) || !strings.Contains(out.String(), `"dry_run":true`) {
		t.Fatalf("unexpected dry-run result: %s", out.String())
	}
	if _, err := os.Stat(filepath.Join(stateDir, "curated.json")); err != nil {
		t.Fatalf("dry-run removed state: %v", err)
	}

	out.Reset()
	errOut.Reset()
	if code := Run(context.Background(), []string{"--home", home, "packs", "remove", "curated"}, &out, &errOut, "dev", "test"); code != 0 {
		t.Fatalf("remove exit %d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), `"removed":true`) || !strings.Contains(out.String(), `"committed":true`) || errOut.Len() != 0 {
		t.Fatalf("unexpected remove result: %s / %s", out.String(), errOut.String())
	}
	if _, err := os.Stat(filepath.Join(home, filepath.FromSlash(item.Filename))); err != nil {
		t.Fatalf("remove deleted original: %v", err)
	}

	out.Reset()
	errOut.Reset()
	if code := Run(context.Background(), []string{"--home", home, "packs", "remove", "curated"}, &out, &errOut, "dev", "test"); code != 0 {
		t.Fatalf("repeated remove exit %d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), `"removed":false`) || !strings.Contains(out.String(), `"retained_bytes":0`) || errOut.Len() != 0 {
		t.Fatalf("unexpected repeated result: %s / %s", out.String(), errOut.String())
	}
}

func TestPackRemoveSchemaDeclaresContract(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"schema", "packs", "remove"}, &out, &errOut, "dev", "test"); code != 0 {
		t.Fatalf("schema exit %d: %s", code, errOut.String())
	}
	for _, want := range []string{`"name":"--dry-run"`, `"retained_bytes"`, `"state_corrupt"`, `sticker packs remove curated`} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("schema is missing %s: %s", want, out.String())
		}
	}
}

func TestPackRepairCLIClearsCorruptState(t *testing.T) {
	home := t.TempDir()
	content := []byte("GIF89a pack repair cli")
	md5Sum := md5.Sum(content)
	shaSum := sha256.Sum256(content)
	id := hex.EncodeToString(md5Sum[:])
	item := library.Item{MD5: id, SHA256: hex.EncodeToString(shaSum[:]), Filename: "emoticons/" + id + ".gif", Format: "gif", Size: int64(len(content)), Caption: "pack caption"}
	manifest := library.Manifest{SchemaVersion: 1, Collection: "curated", Items: []library.Item{item}}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	state := struct {
		SchemaVersion int             `json:"schema_version"`
		ID            string          `json:"id"`
		Source        string          `json:"source"`
		Revision      string          `json:"revision"`
		InstalledAt   time.Time       `json:"installed_at"`
		Manifest      json.RawMessage `json:"manifest"`
	}{1, "curated", "/tmp/source", strings.Repeat("0", 64), time.Now().UTC(), manifestBytes}
	stateBytes, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(home, ".sticker", "packs")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	imagePath := filepath.Join(home, filepath.FromSlash(item.Filename))
	if err := os.MkdirAll(filepath.Dir(imagePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(imagePath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(stateDir, "curated.json")
	if err := os.WriteFile(statePath, stateBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"--home", home, "packs", "repair", "curated", "--dry-run"}, &out, &errOut, "dev", "test"); code != 0 {
		t.Fatalf("repair dry-run exit %d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), `"repaired":true`) || !strings.Contains(out.String(), `"dry_run":true`) {
		t.Fatalf("unexpected repair dry-run result: %s", out.String())
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("dry-run removed corrupt state: %v", err)
	}

	out.Reset()
	errOut.Reset()
	if code := Run(context.Background(), []string{"--home", home, "packs", "repair", "curated"}, &out, &errOut, "dev", "test"); code != 0 {
		t.Fatalf("repair exit %d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), `"repaired":true`) || !strings.Contains(out.String(), `"committed":true`) || errOut.Len() != 0 {
		t.Fatalf("unexpected repair result: %s / %s", out.String(), errOut.String())
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("corrupt state remains after repair: %v", err)
	}
	if _, err := os.Stat(imagePath); err != nil {
		t.Fatalf("repair deleted original image: %v", err)
	}
}

func TestPackRepairSchemaDeclaresContract(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"schema", "packs", "repair"}, &out, &errOut, "dev", "test"); code != 0 {
		t.Fatalf("schema exit %d: %s", code, errOut.String())
	}
	for _, want := range []string{`"command":"packs repair"`, `"name":"--dry-run"`, `"repaired"`, `sticker packs repair all`} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("schema is missing %s: %s", want, out.String())
		}
	}
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
