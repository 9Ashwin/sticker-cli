package library

import (
	"bytes"
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
	header = header[:n]
	switch format {
	case "gif":
		return bytes.HasPrefix(header, []byte("GIF87a")) || bytes.HasPrefix(header, []byte("GIF89a"))
	case "png":
		return bytes.HasPrefix(header, []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a})
	case "jpg":
		return len(header) >= 3 && header[0] == 0xff && header[1] == 0xd8 && header[2] == 0xff
	case "webp":
		return len(header) >= 12 && bytes.Equal(header[:4], []byte("RIFF")) && bytes.Equal(header[8:12], []byte("WEBP"))
	default:
		return false
	}
}
