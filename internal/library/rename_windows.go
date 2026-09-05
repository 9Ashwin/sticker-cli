//go:build windows

package library

import (
	"syscall"
	"unsafe"
)

var moveFileExProc = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")

const moveFileReplaceExisting = 0x1
const moveFileWriteThrough = 0x8

// MoveFileEx replaces the destination in one filesystem operation and asks
// Windows to flush the move before returning.
func renameAtomic(from, to string) error {
	fromPtr, err := syscall.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	toPtr, err := syscall.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	result, _, callErr := moveFileExProc.Call(uintptr(unsafe.Pointer(fromPtr)), uintptr(unsafe.Pointer(toPtr)), moveFileReplaceExisting|moveFileWriteThrough)
	if result == 0 {
		return callErr
	}
	return nil
}

func syncDirectory(string) error { return nil }
