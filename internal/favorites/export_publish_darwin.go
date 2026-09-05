//go:build darwin

package favorites

import "golang.org/x/sys/unix"

func renameExportNoReplace(parentFD int, staging, destination string) error {
	return unix.RenameatxNp(parentFD, staging, parentFD, destination, unix.RENAME_EXCL)
}
