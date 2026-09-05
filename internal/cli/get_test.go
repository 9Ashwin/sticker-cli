package cli

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/9Ashwin/sticker-cli/internal/library"
)

func TestGetReturnsVerifiedAbsoluteItemWithoutImageBytes(t *testing.T) {
	home := t.TempDir()
	data := []byte("GIF89a verified get")
	item := writeGetFixture(t, home, data, "gif")

	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"--home", home, "get", item.MD5}, &out, &errOut, "dev", "test"); code != 0 {
		t.Fatalf("get exit %d: %s", code, errOut.String())
	}
	var response struct {
		OK   bool `json:"ok"`
		Data struct {
			Item struct {
				ID       string   `json:"id"`
				MD5      string   `json:"md5"`
				SHA256   string   `json:"sha256"`
				Format   string   `json:"format"`
				Size     int64    `json:"size"`
				Caption  string   `json:"caption"`
				Path     string   `json:"path"`
				Favorite bool     `json:"favorite"`
				Packs    []string `json:"packs"`
			} `json:"item"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(home, filepath.FromSlash(item.Filename))
	if !response.OK || response.Data.Item.ID != item.MD5 || response.Data.Item.MD5 != item.MD5 || response.Data.Item.SHA256 != item.SHA256 || response.Data.Item.Format != "gif" || response.Data.Item.Size != item.Size || response.Data.Item.Caption != item.Caption || response.Data.Item.Path != wantPath || !filepath.IsAbs(response.Data.Item.Path) || !response.Data.Item.Favorite || len(response.Data.Item.Packs) != 0 {
		t.Fatalf("unexpected get response: %s", out.String())
	}
	if bytes.Contains(out.Bytes(), data) || bytes.Contains(out.Bytes(), []byte(`"base64"`)) || errOut.Len() != 0 {
		t.Fatalf("get leaked image data or wrote stderr: %s / %s", out.String(), errOut.String())
	}
	got, err := os.ReadFile(wantPath)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("get changed the original GIF: %v", err)
	}
}

func TestGetReturnsStableIntegrityErrors(t *testing.T) {
	for _, test := range []struct {
		name    string
		mutate  func(string)
		subtype string
	}{
		{
			name: "missing",
			mutate: func(path string) {
				_ = os.Remove(path)
			},
			subtype: "invalid_image",
		},
		{
			name: "hash mismatch",
			mutate: func(path string) {
				if err := os.WriteFile(path, []byte("GIF89a changed"), 0o600); err != nil {
					panic(err)
				}
			},
			subtype: "hash_mismatch",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			item := writeGetFixture(t, home, []byte("GIF89a verified get"), "gif")
			path := filepath.Join(home, filepath.FromSlash(item.Filename))
			test.mutate(path)

			var out, errOut bytes.Buffer
			if code := Run(context.Background(), []string{"--home", home, "get", item.MD5}, &out, &errOut, "dev", "test"); code != 5 || out.Len() != 0 {
				t.Fatalf("get exit %d stdout=%q stderr=%q", code, out.String(), errOut.String())
			}
			var response errorEnvelope
			if err := json.Unmarshal(errOut.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.OK || response.Error.Type != "integrity" || response.Error.Subtype != test.subtype || response.Error.Hint == "" {
				t.Fatalf("unexpected error response: %s", errOut.String())
			}
		})
	}
}

func TestGetStaticWebPGeneratesAndReusesPreview(t *testing.T) {
	home := t.TempDir()
	data, err := base64.StdEncoding.DecodeString("UklGRhwAAABXRUJQVlA4TA8AAAAvAUAAAAcQ/Y/+ByKi/wEA")
	if err != nil {
		t.Fatal(err)
	}
	item := writeGetFixture(t, home, data, "webp")
	originalPath := filepath.Join(home, filepath.FromSlash(item.Filename))
	originalBefore, err := os.ReadFile(originalPath)
	if err != nil {
		t.Fatal(err)
	}

	get := func() (string, error) {
		var out, errOut bytes.Buffer
		if code := Run(context.Background(), []string{"--home", home, "get", item.MD5, "--preview"}, &out, &errOut, "dev", "test"); code != 0 {
			return "", errors.New(errOut.String())
		}
		var response struct {
			Data struct {
				Item struct {
					Path        string `json:"path"`
					PreviewPath string `json:"preview_path"`
					MD5         string `json:"md5"`
					SHA256      string `json:"sha256"`
					Format      string `json:"format"`
				} `json:"item"`
			} `json:"data"`
		}
		if err := json.Unmarshal(out.Bytes(), &response); err != nil {
			return "", err
		}
		if response.Data.Item.Path != originalPath || response.Data.Item.MD5 != item.MD5 || response.Data.Item.SHA256 != item.SHA256 || response.Data.Item.Format != "webp" || response.Data.Item.PreviewPath == "" || !filepath.IsAbs(response.Data.Item.PreviewPath) {
			return "", errors.New("get response did not contain the verified WebP and preview paths")
		}
		return response.Data.Item.PreviewPath, nil
	}

	previewPath, err := get()
	if err != nil {
		t.Fatal(err)
	}
	previewBefore, err := os.ReadFile(previewPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(previewBefore, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("preview is not PNG: %x", previewBefore[:min(len(previewBefore), 16)])
	}
	previewInfo, err := os.Stat(previewPath)
	if err != nil {
		t.Fatal(err)
	}
	previewAgain, err := get()
	if err != nil || previewAgain != previewPath {
		t.Fatalf("preview was not reused: %s", err)
	}
	previewInfoAgain, err := os.Stat(previewPath)
	if err != nil {
		t.Fatal(err)
	}
	if !previewInfo.ModTime().Equal(previewInfoAgain.ModTime()) {
		t.Fatal("reusing a preview rewrote the cache")
	}
	originalAfter, err := os.ReadFile(originalPath)
	if err != nil || !bytes.Equal(originalBefore, originalAfter) {
		t.Fatalf("preview generation changed the original WebP: %v", err)
	}
}

func TestGetAnimatedWebPPreviewReturnsUnsupportedFormat(t *testing.T) {
	home := t.TempDir()
	data := []byte("RIFF\x1e\x00\x00\x00WEBPVP8X\x0a\x00\x00\x00\x02\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00ANMF\x00\x00\x00\x00")
	item := writeGetFixture(t, home, data, "webp")
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"--home", home, "get", item.MD5, "--preview"}, &out, &errOut, "dev", "test"); code != 2 || out.Len() != 0 {
		t.Fatalf("animated WebP exit %d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	var response errorEnvelope
	if err := json.Unmarshal(errOut.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.OK || response.Error.Type != "validation" || response.Error.Subtype != "unsupported_format" || response.Error.Hint == "" {
		t.Fatalf("unexpected animated WebP error: %s", errOut.String())
	}
}

func writeGetFixture(t *testing.T, root string, data []byte, format string) library.Item {
	t.Helper()
	md5Sum := md5.Sum(data)
	shaSum := sha256.Sum256(data)
	id := hex.EncodeToString(md5Sum[:])
	item := library.Item{
		MD5:      id,
		SHA256:   hex.EncodeToString(shaSum[:]),
		Filename: filepath.ToSlash(filepath.Join(library.EmoticonsDirectory, id+"."+format)),
		Format:   format,
		Size:     int64(len(data)),
		Caption:  "verified fixture",
	}
	if err := os.MkdirAll(filepath.Join(root, library.EmoticonsDirectory), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(item.Filename)), data, 0o600); err != nil {
		t.Fatal(err)
	}
	libraryRoot, err := library.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := libraryRoot.WriteManifest(context.Background(), library.Manifest{SchemaVersion: 1, Collection: "personal", Items: []library.Item{item}}); err != nil {
		t.Fatal(err)
	}
	return item
}
