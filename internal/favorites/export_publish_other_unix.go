//go:build !windows && !linux && !darwin

package favorites

import (
	"errors"
	"os"
)

func renameExportNoReplace(_ int, staging, destination string) error {
	if _, err := os.Lstat(destination); err == nil {
		return errExportDestinationExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(staging, destination)
}
