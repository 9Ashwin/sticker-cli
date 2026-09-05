//go:build windows

package library

import "os"

// os.Rename uses MoveFileEx with replacement semantics on Windows.
func renameAtomic(from, to string) error { return os.Rename(from, to) }

func syncDirectory(string) error { return nil }
