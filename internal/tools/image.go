package tools

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// imageExtensions maps file extensions to MIME media types.
var imageExtensions = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".bmp":  "image/bmp",
}

// imagePathRe matches file paths ending with an image extension.
// Handles: /unix/paths, C:\windows\paths, relative/paths, "quoted paths"
var imagePathRe = regexp.MustCompile(`(?i)(?:"([^"]+\.(?:png|jpe?g|gif|webp|bmp))"|([^\s"]+\.(?:png|jpe?g|gif|webp|bmp)))`)

// MediaTypeFromExt returns the MIME type for an image file extension, or "" if not an image.
func MediaTypeFromExt(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	return imageExtensions[ext]
}

// ExtractImagePaths scans text for file paths ending in image extensions.
// Returns the list of paths that exist on disk and the remaining text with paths removed.
func ExtractImagePaths(text string) (paths []string, remaining string) {
	matches := imagePathRe.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return nil, text
	}

	var found []string
	result := text
	// Process matches in reverse so index shifting doesn't affect earlier matches.
	for i := len(matches) - 1; i >= 0; i-- {
		m := matches[i]
		fullStart, fullEnd := m[0], m[1]

		// Extract the path from whichever capture group matched.
		var path string
		if m[2] >= 0 { // quoted group
			path = text[m[2]:m[3]]
		} else { // unquoted group
			path = text[m[4]:m[5]]
		}

		if !fileExists(path) {
			continue
		}

		found = append([]string{path}, found...)
		result = result[:fullStart] + result[fullEnd:]
	}

	// Collapse multiple spaces left by path removal.
	result = strings.Join(strings.Fields(result), " ")
	return found, result
}

// fileExists reports whether path exists and is a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
