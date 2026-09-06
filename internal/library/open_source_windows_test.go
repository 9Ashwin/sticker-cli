//go:build windows

package library

import "testing"

func TestSameWindowsPathIgnoresCase(t *testing.T) {
	if !sameWindowsPath(`C:\Users\Runner\Temp\Image.GIF`, `c:/users/runner/temp/image.gif`) {
		t.Fatal("Windows path comparison should ignore case and separator style")
	}
}
