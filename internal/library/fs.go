package library

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func (l *Library) ensureRoot(create bool) error {
	info, err := os.Lstat(l.Root)
	if errors.Is(err, os.ErrNotExist) && create {
		if err := os.MkdirAll(l.Root, 0o700); err != nil {
			return wrapError("io", "write_failed", "Choose a writable library directory.", err)
		}
		info, err = os.Lstat(l.Root)
	}
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return wrapError("io", "read_failed", "Check the library directory.", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errorf("validation", "unsafe_path", "Choose a real directory for the library.", "library root is not a directory")
	}
	if create {
		if err := l.ensureDirectory(filepath.Join(l.Root, ".sticker")); err != nil {
			return err
		}
		if err := l.ensureDirectory(filepath.Join(l.Root, EmoticonsDirectory)); err != nil {
			return err
		}
	}
	return nil
}

func (l *Library) ensureDirectory(path string) error {
	if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return wrapError("io", "write_failed", "Choose a writable library directory.", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return wrapError("io", "read_failed", "Check the library directory.", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errorf("validation", "unsafe_path", "Remove links from the library path.", "library path component is not a directory")
	}
	return nil
}

func (l *Library) rootPath(relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) || filepath.Clean(relative) != relative || filepath.VolumeName(relative) != "" {
		return "", errorf("validation", "unsafe_path", "Use a path inside the library root.", "path escapes library root")
	}
	path := filepath.Join(l.Root, relative)
	rel, err := filepath.Rel(l.Root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errorf("validation", "unsafe_path", "Use a path inside the library root.", "path escapes library root")
	}
	return path, nil
}

func (l *Library) itemPath(item Item) (string, error) {
	if filepath.FromSlash(item.Filename) != filepath.Join(EmoticonsDirectory, item.MD5+"."+item.Format) {
		return "", errorf("validation", "unsafe_path", "Use the manifest filename under emoticons/.", "item filename escapes library root")
	}
	path, err := l.rootPath(filepath.FromSlash(item.Filename))
	if err != nil {
		return "", err
	}
	if err := rejectSymlinkComponents(l.Root, path); err != nil {
		return "", err
	}
	return path, nil
}

func rejectSymlinkComponents(root, target string) error {
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errorf("validation", "unsafe_path", "Use a path inside the library root.", "path escapes library root")
	}
	current := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				continue
			}
			return wrapError("io", "read_failed", "Check the library path.", statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errorf("validation", "unsafe_path", "Remove symlinks from the library path.", "path component is a symbolic link")
		}
	}
	return nil
}

func readBounded(ctx context.Context, path string, limit int64) ([]byte, error) {
	file, err := openNoFollow(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	var buffer bytes.Buffer
	if _, err := copyContext(ctx, &buffer, io.LimitReader(file, limit+1)); err != nil {
		return nil, err
	}
	if int64(buffer.Len()) > limit {
		return nil, errorf("integrity", "invalid_manifest", "Use a smaller manifest.", "file exceeds the %d byte limit", limit)
	}
	return buffer.Bytes(), nil
}

func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := scanJSONValue(decoder); err != nil {
		return err
	}
	var token any
	if err := decoder.Decode(&token); err != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

func scanJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok {
				return errors.New("invalid object key")
			}
			if _, exists := seen[name]; exists {
				return errors.New("duplicate object key")
			}
			seen[name] = struct{}{}
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
	case '[':
		for decoder.More() {
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
	}
	return err
}
