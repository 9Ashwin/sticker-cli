// Package preview creates bounded, local previews for image formats that a
// client may not display directly.
package preview

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/9Ashwin/sticker-cli/internal/library"
	"golang.org/x/image/webp"
)

const (
	previewDirectory = ".sticker/previews"
	previewBytes     = library.MaxImageBytes
)

// Generate reads a verified WebP stream and returns the absolute path of a
// cached PNG preview. Non-WebP items do not need a preview and return an empty
// path. The original stream is never written or modified.
func Generate(ctx context.Context, root string, item library.Item, original io.Reader) (string, error) {
	if item.Format != "webp" {
		return "", nil
	}
	if original == nil {
		return "", previewError("io", "read_failed", "Check the image file.", "verified image stream is unavailable")
	}
	if !validSHA256(item.SHA256) {
		return "", previewError("integrity", "invalid_manifest", "Repair the manifest before generating a preview.", "item has an invalid sha256")
	}
	relative := filepath.ToSlash(filepath.Join(previewDirectory, item.SHA256+".png"))
	absolute := filepath.Join(root, filepath.FromSlash(relative))
	if cached, err := readCache(ctx, root, relative); err != nil {
		return "", err
	} else if cached {
		return absolute, nil
	}

	data, err := readBounded(ctx, original, item.Size)
	if err != nil {
		return "", err
	}
	if int64(len(data)) != item.Size {
		return "", previewError("integrity", "hash_mismatch", "Restore the original image or remove the stale entry.", "image changed during preview generation")
	}
	animated, err := inspectWebP(data)
	if err != nil {
		return "", err
	}
	if animated {
		return "", previewError("validation", "unsupported_format", "Use the original image path when animation is required.", "animated WebP cannot be previewed as a static PNG")
	}
	decoded, err := webp.Decode(bytes.NewReader(data))
	if err != nil {
		return "", previewError("integrity", "invalid_image", "Use the original image or repair the source file.", "WebP preview decoding failed")
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, decoded); err != nil {
		return "", previewError("integrity", "invalid_image", "Use the original image or repair the source file.", "PNG preview encoding failed")
	}
	if len(encoded.Bytes()) > previewBytes {
		return "", previewError("validation", "output_limit", "Use the original image path or a smaller image.", "PNG preview exceeds the supported size")
	}
	if err := writeCache(ctx, root, relative, encoded.Bytes()); err != nil {
		return "", err
	}
	return absolute, nil
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func readCache(ctx context.Context, root, relative string) (bool, error) {
	data, err := library.ReadRelative(ctx, root, relative, previewBytes)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false, previewError("cancelled", "interrupted", "Retry the operation when ready.", "preview read cancelled")
		}
		var libraryErr *library.Error
		if errors.As(err, &libraryErr) {
			return false, err
		}
		return false, previewError("io", "read_failed", "Check the preview cache permissions.", "cannot read the preview cache")
	}
	if _, err := png.DecodeConfig(bytes.NewReader(data)); err != nil {
		return false, nil
	}
	return true, nil
}

func readBounded(ctx context.Context, reader io.Reader, size int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, previewError("cancelled", "interrupted", "Retry the operation when ready.", "preview read cancelled")
	}
	if size <= 0 || size > previewBytes {
		return nil, previewError("integrity", "invalid_image", "Use the original image or repair the source file.", "image size is outside the preview limit")
	}
	data, err := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, reader: reader}, size+1))
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, previewError("cancelled", "interrupted", "Retry the operation when ready.", "preview read cancelled")
		}
		return nil, previewError("io", "read_failed", "Check the image file.", "cannot read the image for preview")
	}
	if int64(len(data)) > size {
		return nil, previewError("integrity", "hash_mismatch", "Restore the original image or remove the stale entry.", "image exceeds its declared size")
	}
	return data, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func inspectWebP(data []byte) (bool, error) {
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return false, previewError("integrity", "invalid_image", "Use the original image or repair the source file.", "WebP container header is invalid")
	}
	riffSize := uint64(binary.LittleEndian.Uint32(data[4:8]))
	end := riffSize + 8
	if end < 12 || end > uint64(len(data)) {
		return false, previewError("integrity", "invalid_image", "Use the original image or repair the source file.", "WebP container is truncated")
	}
	for offset := uint64(12); offset < end; {
		if end-offset < 8 {
			return false, previewError("integrity", "invalid_image", "Use the original image or repair the source file.", "WebP chunk header is truncated")
		}
		chunk := data[offset : offset+4]
		chunkSize := uint64(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		chunkEnd := offset + 8 + chunkSize
		if chunkEnd > end {
			return false, previewError("integrity", "invalid_image", "Use the original image or repair the source file.", "WebP chunk exceeds the container")
		}
		if string(chunk) == "ANIM" || string(chunk) == "ANMF" {
			return true, nil
		}
		if string(chunk) == "VP8X" && chunkSize >= 1 && data[offset+8]&0x02 != 0 {
			return true, nil
		}
		offset = chunkEnd + chunkSize&1
		if offset > end {
			return false, previewError("integrity", "invalid_image", "Use the original image or repair the source file.", "WebP chunk padding is invalid")
		}
	}
	return false, nil
}

func writeCache(ctx context.Context, root, relative string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return previewError("cancelled", "interrupted", "Retry the operation when ready.", "preview write cancelled")
	}
	if err := ensurePreviewDirectory(root); err != nil {
		return err
	}
	rootLibrary, err := library.New(root)
	if err != nil {
		return previewError("validation", "unsafe_path", "Choose a local data directory.", "cannot open the preview library")
	}
	if err := rootLibrary.WriteRelativeAtomic(ctx, relative, data); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return previewError("cancelled", "interrupted", "Retry the operation when ready.", "preview write cancelled")
		}
		var libraryErr *library.Error
		if errors.As(err, &libraryErr) && libraryErr.Kind == "validation" {
			return previewError("validation", libraryErr.Subtype, "Remove links from the local data directory.", "preview cache path is unsafe")
		}
		if cached, cacheErr := readCache(ctx, root, relative); cacheErr == nil && cached {
			return nil
		}
		return previewError("io", "write_failed", "Choose a writable local data directory.", "cannot publish the preview cache")
	}
	return nil
}

func ensurePreviewDirectory(root string) error {
	if err := ensureDirectory(root); err != nil {
		return err
	}
	sticker := filepath.Join(root, ".sticker")
	if err := ensureDirectory(sticker); err != nil {
		return err
	}
	return ensureDirectory(filepath.Join(sticker, "previews"))
}

func ensureDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return previewError("io", "write_failed", "Choose a writable local data directory.", "cannot create the preview directory")
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return previewError("io", "read_failed", "Check the local data directory.", "cannot inspect the preview directory")
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return previewError("validation", "unsafe_path", "Remove links from the local data directory.", "preview path is not a real directory")
	}
	return nil
}

func previewError(kind, subtype, hint, message string) *library.Error {
	return &library.Error{Kind: kind, Subtype: subtype, Hint: hint, Message: message}
}
