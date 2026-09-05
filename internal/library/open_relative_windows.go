//go:build windows

package library

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var getFinalPathProc = syscall.NewLazyDLL("kernel32.dll").NewProc("GetFinalPathNameByHandleW")

// Windows checks every component before opening and rejects reparse points
// reported by Lstat. Native Windows CI exercises this path; the final open is
// also performed through the normal handle API so the returned handle is stable.
func openRelativeNoFollow(root, relative string) (*os.File, error) {
	target := filepath.Join(root, relative)
	if err := rejectSymlinkComponents(root, target); err != nil {
		return nil, err
	}
	file, err := openNoFollow(target)
	if err != nil {
		return nil, err
	}
	final, err := finalPath(file)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	normalizedRoot := strings.TrimRight(strings.TrimPrefix(filepath.Clean(root), `\\?\`), `\\`)
	normalizedFinal := strings.TrimRight(strings.TrimPrefix(filepath.Clean(final), `\\?\`), `\\`)
	if !strings.EqualFold(normalizedFinal, normalizedRoot) && !strings.HasPrefix(strings.ToLower(normalizedFinal), strings.ToLower(normalizedRoot)+`\`) {
		_ = file.Close()
		return nil, errorf("validation", "unsafe_path", "Remove links from the library path.", "opened path escapes library root")
	}
	return file, nil
}

func finalPath(file *os.File) (string, error) {
	buffer := make([]uint16, 32768)
	length, _, err := getFinalPathProc.Call(file.Fd(), uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if length == 0 {
		return "", err
	}
	return syscall.UTF16ToString(buffer[:length]), nil
}
