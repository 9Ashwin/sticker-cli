package library

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ReadImage reads and validates one local original image. The returned item
// uses the standard emoticons/<md5>.<format> filename and has an empty caption.
// The returned bytes are a snapshot of the opened file and can be staged for
// a later manifest update even when the source path is subsequently moved.
func ReadImage(ctx context.Context, path string) (Item, []byte, error) {
	if err := contextErr(ctx); err != nil {
		return Item{}, nil, err
	}
	if path == "" {
		return Item{}, nil, errorf("validation", "invalid_argument", "Provide a local image path.", "image path is empty")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return Item{}, nil, wrapError("validation", "unsafe_path", "Choose a local image path.", err)
	}
	absolute = filepath.Clean(absolute)
	info, err := os.Lstat(absolute)
	if err != nil {
		return Item{}, nil, wrapError("io", "read_failed", "Check the source image path.", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return Item{}, nil, errorf("validation", "unsafe_path", "Provide the original image file, not a symbolic link.", "source image is a symbolic link")
	}
	if !info.Mode().IsRegular() {
		return Item{}, nil, errorf("validation", "unsafe_path", "Provide a regular image file.", "source image is not a regular file")
	}

	file, err := openNoFollow(absolute)
	if err != nil {
		var coded *Error
		if errors.As(err, &coded) {
			return Item{}, nil, err
		}
		return Item{}, nil, wrapError("io", "read_failed", "Check the source image permissions.", err)
	}
	defer func() { _ = file.Close() }()

	data, err := readImageBytes(ctx, file)
	if err != nil {
		return Item{}, nil, err
	}
	finalInfo, err := file.Stat()
	if err != nil {
		return Item{}, nil, wrapError("io", "read_failed", "Check the source image.", err)
	}
	if finalInfo.Size() != int64(len(data)) {
		return Item{}, nil, errorf("integrity", "hash_mismatch", "Retry with an unchanged source image.", "source image changed while it was being read")
	}

	format := imageFormatFromPath(absolute)
	if format == "" {
		format = detectImageFormat(data)
	}
	if format == "" {
		return Item{}, nil, errorf("validation", "unsupported_format", "Use a gif, png, jpg, or webp image.", "could not determine the source image format")
	}
	if !hasImageSignatureBytes(data, format) {
		return Item{}, nil, errorf("integrity", "invalid_image", "Provide the original image in its declared format.", "source image does not have a %s signature", format)
	}

	md5Sum := md5.Sum(data)
	shaSum := sha256.Sum256(data)
	id := hex.EncodeToString(md5Sum[:])
	item := Item{
		MD5:      id,
		SHA256:   hex.EncodeToString(shaSum[:]),
		Filename: filepath.ToSlash(filepath.Join(EmoticonsDirectory, id+"."+format)),
		Format:   format,
		Size:     int64(len(data)),
	}
	return item, data, nil
}

func readImageBytes(ctx context.Context, file *os.File) ([]byte, error) {
	var buffer bytes.Buffer
	if _, err := copyContext(ctx, &buffer, io.LimitReader(file, DefaultLimits().ImageBytes+1)); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, errorf("cancelled", "interrupted", "Retry the operation when ready.", "source image read cancelled")
		}
		return nil, wrapError("io", "read_failed", "Check the source image.", err)
	}
	if int64(buffer.Len()) > DefaultLimits().ImageBytes {
		return nil, errorf("integrity", "invalid_image", "Use an image within the supported size limit.", "source image exceeds the %d byte limit", DefaultLimits().ImageBytes)
	}
	return buffer.Bytes(), nil
}

func imageFormatFromPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".gif":
		return "gif"
	case ".png":
		return "png"
	case ".jpg", ".jpeg":
		return "jpg"
	case ".webp":
		return "webp"
	default:
		return ""
	}
}

func detectImageFormat(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return "gif"
	case bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}):
		return "png"
	case len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff:
		return "jpg"
	case len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return "webp"
	default:
		return ""
	}
}

func hasImageSignatureBytes(data []byte, format string) bool {
	switch format {
	case "gif":
		return bytes.HasPrefix(data, []byte("GIF87a")) || bytes.HasPrefix(data, []byte("GIF89a"))
	case "png":
		return bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a})
	case "jpg":
		return len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff
	case "webp":
		return len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP"))
	default:
		return false
	}
}
