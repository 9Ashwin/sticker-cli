//go:build windows

package library

import (
	"os"
	"path/filepath"
	"strings"
)

func openAbsoluteNoFollow(path string) (*os.File, error) {
	if !filepath.IsAbs(path) {
		return nil, errorf("validation", "unsafe_path", "Use an absolute local image path.", "source image path is not absolute")
	}
	if err := rejectSymlinkComponents(filepath.VolumeName(path)+string(filepath.Separator), path); err != nil {
		return nil, err
	}
	return openNoFollow(path)
}

func validateSourceSymlinks(original, canonical string) error {
	if !filepath.IsAbs(original) || !filepath.IsAbs(canonical) || !sameWindowsPath(original, canonical) {
		return errorf("validation", "unsafe_path", "Remove links from the source image path.", "source image path changed while resolving directory components")
	}
	return rejectSymlinkComponents(filepath.VolumeName(original)+string(filepath.Separator), original)
}

func sameWindowsPath(first, second string) bool {
	// EvalSymlinks returns the filesystem's canonical casing on Windows.
	return strings.EqualFold(filepath.Clean(first), filepath.Clean(second))
}
