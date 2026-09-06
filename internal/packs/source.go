package packs

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// DefaultSource is the public source used when --source is omitted.
const DefaultSource = "https://raw.githubusercontent.com/9Ashwin/sticker-ext/main"

const (
	cacheDirectory = ".sticker/catalogs"
	cacheTTL       = 24 * time.Hour
)

// Source identifies a public HTTPS source or a local source directory.
type Source struct {
	Canonical string
	LocalRoot string
	URL       *url.URL
}

func (s Source) IsLocal() bool { return s.LocalRoot != "" }

// Resolve normalizes and validates a source without requiring local paths to
// exist. This lets a cached local catalog remain usable with --offline after
// the source directory has been moved or disconnected.
func Resolve(raw string) (Source, error) {
	if raw == "" {
		raw = DefaultSource
	}
	if strings.IndexByte(raw, 0) >= 0 {
		return Source{}, newError("validation", "invalid_argument", "source contains an invalid character", "Use an HTTPS URL or a local directory path.")
	}
	if strings.HasPrefix(strings.ToLower(raw), "http://") {
		return Source{}, newError("validation", "invalid_argument", "source must use HTTPS", "Use an HTTPS source URL.")
	}
	if isLocalSourcePath(raw) {
		return resolveLocalSource(raw)
	}
	if parsed, err := url.Parse(raw); err == nil && parsed.Scheme != "" {
		return resolveURLSource(parsed)
	}
	if strings.HasPrefix(raw, "//") || strings.Contains(raw, "\\") {
		return Source{}, newError("validation", "invalid_argument", "source must be an HTTPS URL or local directory", "Use an absolute or relative local directory path.")
	}
	return resolveLocalSource(raw)
}

func isLocalSourcePath(raw string) bool {
	if filepath.IsAbs(raw) || filepath.VolumeName(raw) != "" {
		return true
	}
	return filepath.Separator == '\\' && strings.ContainsRune(raw, '\\') && !strings.Contains(raw, "://")
}

func resolveLocalSource(raw string) (Source, error) {
	abs, err := filepath.Abs(raw)
	if err != nil {
		return Source{}, wrapError("validation", "unsafe_path", "cannot resolve source directory", "Use a valid local directory path.", err)
	}
	return Source{Canonical: filepath.Clean(abs), LocalRoot: filepath.Clean(abs)}, nil
}

func resolveURLSource(parsed *url.URL) (Source, error) {
	if !strings.EqualFold(parsed.Scheme, "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return Source{}, newError("validation", "invalid_argument", "source must be an HTTPS URL without credentials or query parameters", "Remove userinfo, query, and fragment components from the source URL.")
	}
	if strings.Contains(parsed.Path, "\\") || hasDotPathComponent(parsed.Path) {
		return Source{}, newError("validation", "invalid_argument", "source URL contains an unsafe path", "Use a source URL without dot path components.")
	}
	parsed.Scheme = "https"
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""
	return Source{Canonical: parsed.String(), URL: parsed}, nil
}

func hasDotPathComponent(value string) bool {
	for _, part := range strings.Split(value, "/") {
		if part == "." || part == ".." {
			return true
		}
	}
	return false
}

func (s Source) cacheKey() string {
	sum := sha256.Sum256([]byte(s.Canonical))
	return hex.EncodeToString(sum[:])
}

func (s Source) cachePath(home string) string {
	return filepath.Join(home, cacheDirectory, s.cacheKey()+".json")
}

func (s Source) validateLocal() error {
	info, err := os.Lstat(s.LocalRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return newError("not_found", "source_not_found", "source directory was not found", "Check the local source path.")
		}
		return wrapError("io", "read_failed", "cannot inspect source directory", "Check the local source permissions.", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return newError("validation", "unsafe_path", "source must be a real directory", "Choose a local directory without symbolic links.")
	}
	return nil
}

func (s Source) manifestURL(relative string) string {
	if s.URL == nil {
		return ""
	}
	copyURL := *s.URL
	copyURL.Path = path.Join(strings.TrimRight(copyURL.Path, "/"), relative)
	copyURL.RawPath = ""
	return copyURL.String()
}
