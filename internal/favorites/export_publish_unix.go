//go:build !windows

package favorites

import (
	"errors"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

var errExportDestinationExists = errors.New("export destination exists")

func publishDirectoryNoReplace(staging, destination string) error {
	parent := filepath.Dir(destination)
	parentFD, err := openExportParent(parent)
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(parentFD) }()

	tempName := filepath.Base(staging)
	targetName := filepath.Base(destination)
	if tempName == "." || tempName == ".." || targetName == "." || targetName == ".." || strings.ContainsAny(tempName, `/\`) || strings.ContainsAny(targetName, `/\`) {
		return errors.New("export path has an invalid final component")
	}
	if err := renameExportNoReplace(parentFD, tempName, targetName); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return errExportDestinationExists
		}
		return err
	}
	if err := unix.Fsync(parentFD); err != nil {
		return &exportCommittedError{err: err}
	}
	return nil
}

func openExportParent(path string) (int, error) {
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return -1, err
	}
	canonical = filepath.Clean(canonical)
	if !filepath.IsAbs(canonical) {
		return -1, errors.New("export destination parent is not absolute")
	}
	rootFD, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	currentFD := rootFD
	closeCurrent := true
	defer func() {
		if closeCurrent {
			_ = unix.Close(currentFD)
		}
	}()
	relative := strings.TrimPrefix(canonical, string(filepath.Separator))
	if relative == "" {
		closeCurrent = false
		return currentFD, nil
	}
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		if part == "" || part == "." || part == ".." {
			return -1, errors.New("export destination parent has an unsafe path component")
		}
		nextFD, openErr := unix.Openat(currentFD, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if openErr != nil {
			return -1, openErr
		}
		_ = unix.Close(currentFD)
		currentFD = nextFD
	}
	closeCurrent = false
	return currentFD, nil
}
