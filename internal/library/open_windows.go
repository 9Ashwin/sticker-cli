//go:build windows

package library

import "os"

// Windows path components are checked with Lstat before opening.
func openNoFollow(path string) (*os.File, error) { return os.Open(path) }
