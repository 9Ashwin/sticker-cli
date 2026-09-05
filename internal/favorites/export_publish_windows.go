//go:build windows

package favorites

import (
	"errors"
	"os"
)

var errExportDestinationExists = errors.New("export destination exists")

func publishDirectoryNoReplace(staging, destination string) error {
	if _, err := os.Lstat(destination); err == nil {
		return errExportDestinationExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(staging, destination); err != nil {
		if errors.Is(err, os.ErrExist) {
			return errExportDestinationExists
		}
		return err
	}
	return nil
}
