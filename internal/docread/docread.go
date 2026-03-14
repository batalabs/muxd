package docread

import (
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ErrUnsupportedFormat is returned when the file extension has no registered extractor.
var ErrUnsupportedFormat = errors.New("unsupported document format")

const (
	maxFileSize   = 10 * 1024 * 1024 // 10 MB
	maxExtractLen = 100_000          // 100k chars
)

// supportedExts lists all extensions with registered extractors.
var supportedExts = map[string]bool{
	".pdf":  true,
	".docx": true,
	".xlsx": true,
	".pptx": true,
	".html": true,
	".htm":  true,
	".csv":  true,
	".json": true,
	".xml":  true,
}

// CanExtract reports whether the given file extension is a supported document format.
func CanExtract(ext string) bool {
	return supportedExts[strings.ToLower(ext)]
}

// Extract reads a document file and returns its text content.
// Returns ErrUnsupportedFormat for unrecognized extensions.
// Returns an error if the file exceeds maxFileSize.
// Output is truncated at maxExtractLen characters with a notice appended.
func Extract(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", path, err)
	}
	if info.Size() > maxFileSize {
		return "", fmt.Errorf("file %s is too large (%d bytes; limit is %d bytes)", path, info.Size(), maxFileSize)
	}

	ext := strings.ToLower(filepath.Ext(path))

	var text string
	switch ext {
	case ".csv":
		text, err = extractCSV(path)
	case ".json":
		text, err = extractJSON(path)
	case ".xml":
		text, err = extractXML(path)
	case ".pdf":
		text, err = extractPDF(path)
	case ".docx":
		text, err = extractDOCX(path)
	case ".xlsx":
		text, err = extractXLSX(path)
	case ".pptx":
		text, err = extractPPTX(path)
	case ".html", ".htm":
		text, err = extractHTML(path)
	default:
		return "", ErrUnsupportedFormat
	}
	if err != nil {
		return "", err
	}

	if len(text) > maxExtractLen {
		text = text[:maxExtractLen] + "\n[output truncated at 100 000 characters]"
	}
	return text, nil
}

// extractCSV reads a CSV file and returns its content as tab-separated rows.
func extractCSV(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open csv %s: %w", path, err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	var sb strings.Builder
	for {
		record, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("read csv %s: %w", path, err)
		}
		sb.WriteString(strings.Join(record, "\t"))
		sb.WriteByte('\n')
	}
	return sb.String(), nil
}

// extractJSON reads a JSON file and returns a pretty-printed representation.
func extractJSON(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read json %s: %w", path, err)
	}

	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return "", fmt.Errorf("parse json %s: %w", path, err)
	}

	pretty, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal json %s: %w", path, err)
	}
	return string(pretty), nil
}

// extractXML reads an XML file and returns only the text content nodes, with tags stripped.
func extractXML(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open xml %s: %w", path, err)
	}
	defer f.Close()

	dec := xml.NewDecoder(f)
	var sb strings.Builder
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("parse xml %s: %w", path, err)
		}
		if charData, ok := tok.(xml.CharData); ok {
			text := strings.TrimSpace(string(charData))
			if text != "" {
				sb.WriteString(text)
				sb.WriteByte('\n')
			}
		}
	}
	return sb.String(), nil
}
