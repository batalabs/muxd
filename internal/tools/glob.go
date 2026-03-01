package tools

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/batalabs/muxd/internal/provider"
)

// ---------------------------------------------------------------------------
// glob — recursive file pattern matching with ** support
// ---------------------------------------------------------------------------

func globTool() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "glob",
			Description: "Find files by glob pattern. Supports ** for recursive directory matching (e.g. '**/*.go', 'src/**/*.test.ts', 'internal/**/server.go'). Results are sorted by modification time (newest first). Use this to locate files before reading or editing them.",
			Properties: map[string]provider.ToolProp{
				"pattern": {Type: "string", Description: "Glob pattern (e.g. '**/*.go', 'src/**/*.ts', '*.json')"},
				"path":    {Type: "string", Description: "Base directory to search from (default: current directory)"},
			},
			Required: []string{"pattern"},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			pattern, ok := input["pattern"].(string)
			if !ok || pattern == "" {
				return "", fmt.Errorf("pattern is required")
			}

			basePath := "."
			if v, ok := input["path"].(string); ok && v != "" {
				basePath = v
			}

			matches, err := globMatch(basePath, pattern)
			if err != nil {
				return "", err
			}

			if len(matches) == 0 {
				return "No files found.", nil
			}

			const maxResults = 500
			truncated := false
			if len(matches) > maxResults {
				matches = matches[:maxResults]
				truncated = true
			}

			result := strings.Join(matches, "\n")
			if truncated {
				result += fmt.Sprintf("\n... (truncated at %d results)", maxResults)
			}
			return result, nil
		},
	}
}

// globMatch finds files matching a glob pattern with ** support.
// Results are sorted by modification time (newest first).
func globMatch(basePath, pattern string) ([]string, error) {
	// If pattern doesn't contain **, use simple filepath.Glob
	if !strings.Contains(pattern, "**") {
		fullPattern := filepath.Join(basePath, pattern)
		matches, err := filepath.Glob(fullPattern)
		if err != nil {
			return nil, fmt.Errorf("invalid glob pattern: %w", err)
		}
		return sortByModTime(matches), nil
	}

	// Split pattern on ** to get prefix and suffix parts.
	// e.g. "src/**/*.go" -> prefix="src", suffix="*.go"
	// e.g. "**/*.go" -> prefix="", suffix="*.go"
	// e.g. "internal/**/server.go" -> prefix="internal", suffix="server.go"
	parts := strings.SplitN(pattern, "**", 2)
	prefix := strings.TrimRight(parts[0], "/\\")
	suffix := strings.TrimLeft(parts[1], "/\\")

	searchRoot := basePath
	if prefix != "" {
		searchRoot = filepath.Join(basePath, prefix)
	}

	// Verify search root exists.
	if _, err := os.Stat(searchRoot); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat %s: %w", searchRoot, err)
	}

	var matches []string
	const maxWalk = 50000 // safety limit on files scanned

	walked := 0
	_ = filepath.WalkDir(searchRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		walked++
		if walked > maxWalk {
			return filepath.SkipAll
		}

		name := d.Name()

		// Skip hidden directories and common generated dirs.
		if d.IsDir() {
			if (strings.HasPrefix(name, ".") && name != ".") || hiddenDirs[name] {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip hidden files.
		if strings.HasPrefix(name, ".") {
			return nil
		}

		// Match the suffix pattern against the file name or relative path.
		if suffix == "" {
			// Pattern was just "**" — match everything.
			matches = append(matches, filepath.ToSlash(path))
			return nil
		}

		// Try matching suffix against just the filename first (most common: **/*.go).
		if matched, _ := filepath.Match(suffix, name); matched {
			matches = append(matches, filepath.ToSlash(path))
			return nil
		}

		// Also try matching against the relative path from search root
		// for patterns like **/foo/bar.go.
		if strings.Contains(suffix, "/") || strings.Contains(suffix, string(filepath.Separator)) {
			rel, relErr := filepath.Rel(searchRoot, path)
			if relErr == nil {
				if matched, _ := filepath.Match(suffix, filepath.ToSlash(rel)); matched {
					matches = append(matches, filepath.ToSlash(path))
					return nil
				}
				// Also try matching each sub-path for nested ** patterns.
				relSlash := filepath.ToSlash(rel)
				parts := strings.Split(relSlash, "/")
				for i := range parts {
					subPath := strings.Join(parts[i:], "/")
					if matched, _ := filepath.Match(suffix, subPath); matched {
						matches = append(matches, filepath.ToSlash(path))
						return nil
					}
				}
			}
		}

		return nil
	})

	return sortByModTime(matches), nil
}

// sortByModTime sorts file paths by modification time (newest first).
func sortByModTime(paths []string) []string {
	type fileWithTime struct {
		path    string
		modTime time.Time
	}

	files := make([]fileWithTime, 0, len(paths))
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			files = append(files, fileWithTime{path: p})
			continue
		}
		files = append(files, fileWithTime{path: p, modTime: info.ModTime()})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})

	result := make([]string, len(files))
	for i, f := range files {
		result[i] = f.path
	}
	return result
}
