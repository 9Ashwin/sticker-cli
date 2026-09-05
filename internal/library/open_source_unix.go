//go:build !windows

package library

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func openAbsoluteNoFollow(path string) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.VolumeName(path) != "" {
		return nil, errorf("validation", "unsafe_path", "Use an absolute local image path.", "source image path is not absolute")
	}
	clean := filepath.Clean(path)
	relative := strings.TrimPrefix(clean, string(filepath.Separator))
	if relative == "" {
		return nil, errorf("validation", "unsafe_path", "Provide a regular image file.", "source image path points to the filesystem root")
	}
	return openRelativeNoFollow(string(filepath.Separator), relative)
}

func validateSourceSymlinks(original, canonical string) error {
	original = filepath.Clean(original)
	if !filepath.IsAbs(original) {
		return errorf("validation", "unsafe_path", "Use an absolute local image path.", "source image path is not absolute")
	}
	if canonicalSourceAlias(original) != canonical {
		return errorf("validation", "unsafe_path", "Remove symlinks from the source image path.", "source image path changed while resolving directory components")
	}
	current := string(filepath.Separator)
	relative := strings.TrimPrefix(original, current)
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		if part == "" || part == "." || part == ".." {
			return errorf("validation", "unsafe_path", "Use a clean absolute local image path.", "source image path contains an unsafe component")
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return wrapError("io", "read_failed", "Check the source image path.", err)
		}
		if info.Mode()&os.ModeSymlink != 0 && !trustedSourceSymlink(current) {
			return errorf("validation", "unsafe_path", "Remove symlinks from the source image path.", "source image path contains a symbolic link")
		}
	}
	return nil
}

func canonicalSourceAlias(path string) string {
	if runtime.GOOS != "darwin" {
		return path
	}
	for _, alias := range []string{"/var", "/tmp", "/etc"} {
		if path == alias || strings.HasPrefix(path, alias+string(filepath.Separator)) {
			return filepath.Join("/private", strings.TrimPrefix(path, "/"))
		}
	}
	return path
}

func trustedSourceSymlink(path string) bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	for _, alias := range []string{"/var", "/tmp", "/etc"} {
		if path != alias {
			continue
		}
		target, err := os.Readlink(path)
		if err != nil {
			return false
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(path), target)
		}
		return strings.HasPrefix(filepath.Clean(target), "/private/")
	}
	return false
}
