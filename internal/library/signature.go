package library

import (
	"io"
	"os"
)

func hasImageSignature(file *os.File, format string) bool {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return false
	}
	header := make([]byte, 12)
	n, err := io.ReadFull(file, header)
	if err != nil && err != io.ErrUnexpectedEOF {
		return false
	}
	return hasImageSignatureBytes(header[:n], format)
}
