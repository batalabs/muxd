package docread

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
)

// extractPPTX extracts text from a PPTX file.
// Each slide is preceded by a "--- Slide N ---" header.
func extractPPTX(filePath string) (string, error) {
	zr, err := zip.OpenReader(filePath)
	if err != nil {
		return "", fmt.Errorf("opening PPTX: %w", err)
	}
	defer zr.Close()

	// Collect slide files and sort them by slide number.
	type slideFile struct {
		num  int
		file *zip.File
	}
	var slides []slideFile
	for _, f := range zr.File {
		dir, base := path.Split(f.Name)
		if dir != "ppt/slides/" || !strings.HasPrefix(base, "slide") {
			continue
		}
		// Skip _rels files.
		if strings.HasSuffix(base, ".rels") {
			continue
		}
		// Parse slideN.xml → N.
		numStr := strings.TrimSuffix(strings.TrimPrefix(base, "slide"), ".xml")
		num, err := strconv.Atoi(numStr)
		if err != nil {
			continue
		}
		slides = append(slides, slideFile{num: num, file: f})
	}
	sort.Slice(slides, func(i, j int) bool { return slides[i].num < slides[j].num })

	var sb strings.Builder
	for _, s := range slides {
		text, err := extractSlideText(s.file)
		if err != nil {
			continue
		}
		sb.WriteString(fmt.Sprintf("--- Slide %d ---\n", s.num))
		sb.WriteString(text)
		sb.WriteByte('\n')
	}
	return sb.String(), nil
}

// extractSlideText reads a single slide XML file and returns its plain text.
func extractSlideText(f *zip.File) (string, error) {
	rc, err := f.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()

	dec := xml.NewDecoder(rc)
	var sb strings.Builder
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			break
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
