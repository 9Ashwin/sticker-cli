//go:build windows

package library

import (
	"os"
	"syscall"
	"unsafe"
)

const (
	genericRead             = 0x80000000
	genericWrite            = 0x40000000
	fileShareRead           = 0x00000001
	fileShareWrite          = 0x00000002
	fileShareDelete         = 0x00000004
	openExisting            = 3
	openAlways              = 4
	fileFlagReparse         = 0x00200000
	fileFlagBackupSemantics = 0x02000000
	fileAttributeReparse    = 0x00000400
)

var (
	createFileProc         = syscall.NewLazyDLL("kernel32.dll").NewProc("CreateFileW")
	getFileInformationProc = syscall.NewLazyDLL("kernel32.dll").NewProc("GetFileInformationByHandle")
)

type byHandleFileInformation struct {
	FileAttributes     uint32
	CreationTime       syscall.Filetime
	LastAccessTime     syscall.Filetime
	LastWriteTime      syscall.Filetime
	VolumeSerialNumber uint32
	FileSizeHigh       uint32
	FileSizeLow        uint32
	NumberOfLinks      uint32
	FileIndexHigh      uint32
	FileIndexLow       uint32
}

// CreateFile with OPEN_REPARSE_POINT returns a handle to a reparse point
// instead of following the final one. The handle metadata check rejects it.
func openNoFollow(path string) (*os.File, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, _, callErr := createFileProc.Call(
		uintptr(unsafe.Pointer(name)), genericRead,
		fileShareRead|fileShareWrite|fileShareDelete, 0, openExisting,
		fileFlagReparse, 0,
	)
	if handle == uintptr(syscall.InvalidHandle) {
		return nil, callErr
	}
	var information byHandleFileInformation
	result, _, infoErr := getFileInformationProc.Call(handle, uintptr(unsafe.Pointer(&information)))
	if result == 0 {
		_ = syscall.CloseHandle(syscall.Handle(handle))
		return nil, infoErr
	}
	if information.FileAttributes&fileAttributeReparse != 0 {
		_ = syscall.CloseHandle(syscall.Handle(handle))
		return nil, &Error{Kind: "validation", Subtype: "unsafe_path", Message: "path contains a reparse point", Hint: "Remove links from the library path."}
	}
	return os.NewFile(handle, path), nil
}

func openDirectoryNoReparse(path string) (*os.File, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, _, callErr := createFileProc.Call(
		uintptr(unsafe.Pointer(name)), genericRead,
		fileShareRead|fileShareWrite, 0, openExisting,
		fileFlagReparse|fileFlagBackupSemantics, 0,
	)
	if handle == uintptr(syscall.InvalidHandle) {
		return nil, callErr
	}
	var information byHandleFileInformation
	result, _, infoErr := getFileInformationProc.Call(handle, uintptr(unsafe.Pointer(&information)))
	if result == 0 {
		_ = syscall.CloseHandle(syscall.Handle(handle))
		return nil, infoErr
	}
	if information.FileAttributes&fileAttributeReparse != 0 {
		_ = syscall.CloseHandle(syscall.Handle(handle))
		return nil, &Error{Kind: "validation", Subtype: "unsafe_path", Message: "path contains a reparse point", Hint: "Remove links from the library path."}
	}
	return os.NewFile(handle, path), nil
}

func openLockNoReparse(path string) (*os.File, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, _, callErr := createFileProc.Call(
		uintptr(unsafe.Pointer(name)), genericRead|genericWrite,
		fileShareRead|fileShareWrite, 0, openAlways,
		fileFlagReparse, 0,
	)
	if handle == uintptr(syscall.InvalidHandle) {
		return nil, callErr
	}
	var information byHandleFileInformation
	result, _, infoErr := getFileInformationProc.Call(handle, uintptr(unsafe.Pointer(&information)))
	if result == 0 {
		_ = syscall.CloseHandle(syscall.Handle(handle))
		return nil, infoErr
	}
	if information.FileAttributes&fileAttributeReparse != 0 {
		_ = syscall.CloseHandle(syscall.Handle(handle))
		return nil, &Error{Kind: "validation", Subtype: "unsafe_path", Message: "path contains a reparse point", Hint: "Remove links from the library path."}
	}
	return os.NewFile(handle, path), nil
}
