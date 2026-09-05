//go:build linux

package favorites

import "golang.org/x/sys/unix"

func renameExportNoReplace(parentFD int, staging, destination string) error {
	return unix.Renameat2(parentFD, staging, parentFD, destination, unix.RENAME_NOREPLACE)
}
