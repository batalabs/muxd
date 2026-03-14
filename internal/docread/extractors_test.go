package docread

import (
	"archive/zip"
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

// makeZip creates an in-memory ZIP archive with the given file entries and writes
// it to a temporary *.ext file. Returns the path.
func makeZip(t *testing.T, ext string, entries map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip.Create %q: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %q: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip.Close: %v", err)
	}
	f, err := os.CreateTemp(t.TempDir(), "test*"+ext)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	return f.Name()
}

// --- PDF ---

func TestExtractPDF(t *testing.T) {
	// Use the real PDF fixture checked into testdata/.
	// The file is sourced from the ledongthuc/pdf library's own test suite.
	path := "testdata/sample.pdf"
	if _, err := os.Stat(path); err != nil {
		t.Skipf("testdata/sample.pdf not found: %v", err)
	}

	got, err := extractPDF(path)
	if err != nil {
		t.Fatalf("extractPDF: %v", err)
	}
	// The fixture contains readable text — just verify extraction produced output.
	if strings.TrimSpace(got) == "" {
		t.Errorf("expected non-empty text from PDF, got empty string")
	}
}

// TestExtractPDF_notFound ensures a missing file is reported as an error.
func TestExtractPDF_notFound(t *testing.T) {
	_, err := extractPDF("/nonexistent/path/file.pdf")
	if err == nil {
		t.Error("expected error for non-existent PDF, got nil")
	}
}

// --- DOCX ---

func TestExtractDOCX(t *testing.T) {
	// A minimal DOCX is a ZIP containing word/document.xml and word/_rels/document.xml.rels.
	const docXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p>
      <w:r><w:t>Hello DOCX World</w:t></w:r>
    </w:p>
  </w:body>
</w:document>`

	const relsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
</Relationships>`

	path := makeZip(t, ".docx", map[string]string{
		"word/document.xml":            docXML,
		"word/_rels/document.xml.rels": relsXML,
	})

	got, err := extractDOCX(path)
	if err != nil {
		t.Fatalf("extractDOCX: %v", err)
	}
	if !strings.Contains(got, "Hello DOCX World") {
		t.Errorf("expected 'Hello DOCX World' in output, got: %q", got)
	}
	if strings.Contains(got, "<w:") {
		t.Errorf("expected XML tags stripped, got: %q", got)
	}
}

func TestExtractDOCX_notFound(t *testing.T) {
	_, err := extractDOCX("/nonexistent/path/file.docx")
	if err == nil {
		t.Error("expected error for non-existent DOCX, got nil")
	}
}

// --- XLSX ---

func TestExtractXLSX(t *testing.T) {
	// Build the XLSX programmatically using excelize.
	f := excelize.NewFile()
	defer f.Close()

	// Sheet1 already exists by default.
	if err := f.SetCellValue("Sheet1", "A1", "Name"); err != nil {
		t.Fatal(err)
	}
	if err := f.SetCellValue("Sheet1", "B1", "Score"); err != nil {
		t.Fatal(err)
	}
	if err := f.SetCellValue("Sheet1", "A2", "Alice"); err != nil {
		t.Fatal(err)
	}
	if err := f.SetCellValue("Sheet1", "B2", 42); err != nil {
		t.Fatal(err)
	}

	path := t.TempDir() + "/sample.xlsx"
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("save xlsx: %v", err)
	}

	got, err := extractXLSX(path)
	if err != nil {
		t.Fatalf("extractXLSX: %v", err)
	}
	if !strings.Contains(got, "Sheet1") {
		t.Errorf("expected sheet name in output, got: %q", got)
	}
	if !strings.Contains(got, "Name") {
		t.Errorf("expected 'Name' in output, got: %q", got)
	}
	if !strings.Contains(got, "Alice") {
		t.Errorf("expected 'Alice' in output, got: %q", got)
	}
	// Rows must be tab-separated.
	if !strings.Contains(got, "Name\tScore") {
		t.Errorf("expected tab-separated header 'Name\\tScore', got: %q", got)
	}
}

func TestExtractXLSX_multipleSheets(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()

	if err := f.SetCellValue("Sheet1", "A1", "First"); err != nil {
		t.Fatal(err)
	}
	idx, err := f.NewSheet("Sheet2")
	if err != nil {
		t.Fatal(err)
	}
	f.SetActiveSheet(idx)
	if err := f.SetCellValue("Sheet2", "A1", "Second"); err != nil {
		t.Fatal(err)
	}

	path := t.TempDir() + "/multi.xlsx"
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("save xlsx: %v", err)
	}

	got, err := extractXLSX(path)
	if err != nil {
		t.Fatalf("extractXLSX: %v", err)
	}
	if !strings.Contains(got, "--- Sheet: Sheet1 ---") {
		t.Errorf("expected Sheet1 header, got: %q", got)
	}
	if !strings.Contains(got, "--- Sheet: Sheet2 ---") {
		t.Errorf("expected Sheet2 header, got: %q", got)
	}
	if !strings.Contains(got, "First") {
		t.Errorf("expected 'First' in output, got: %q", got)
	}
	if !strings.Contains(got, "Second") {
		t.Errorf("expected 'Second' in output, got: %q", got)
	}
}

func TestExtractXLSX_notFound(t *testing.T) {
	_, err := extractXLSX("/nonexistent/path/file.xlsx")
	if err == nil {
		t.Error("expected error for non-existent XLSX, got nil")
	}
}

// --- PPTX ---

func TestExtractPPTX(t *testing.T) {
	// Minimal PPTX: a ZIP with ppt/slides/slide1.xml and ppt/slides/slide2.xml.
	const slide1XML = `<?xml version="1.0" encoding="UTF-8"?>
<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
       xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">
  <p:cSld>
    <p:spTree>
      <p:sp>
        <p:txBody>
          <a:p><a:r><a:t>Hello Slide One</a:t></a:r></a:p>
        </p:txBody>
      </p:sp>
    </p:spTree>
  </p:cSld>
</p:sld>`

	const slide2XML = `<?xml version="1.0" encoding="UTF-8"?>
<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
       xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">
  <p:cSld>
    <p:spTree>
      <p:sp>
        <p:txBody>
          <a:p><a:r><a:t>Slide Two Content</a:t></a:r></a:p>
        </p:txBody>
      </p:sp>
    </p:spTree>
  </p:cSld>
</p:sld>`

	path := makeZip(t, ".pptx", map[string]string{
		"ppt/slides/slide1.xml": slide1XML,
		"ppt/slides/slide2.xml": slide2XML,
	})

	got, err := extractPPTX(path)
	if err != nil {
		t.Fatalf("extractPPTX: %v", err)
	}
	if !strings.Contains(got, "--- Slide 1 ---") {
		t.Errorf("expected '--- Slide 1 ---' header, got: %q", got)
	}
	if !strings.Contains(got, "--- Slide 2 ---") {
		t.Errorf("expected '--- Slide 2 ---' header, got: %q", got)
	}
	if !strings.Contains(got, "Hello Slide One") {
		t.Errorf("expected 'Hello Slide One' in output, got: %q", got)
	}
	if !strings.Contains(got, "Slide Two Content") {
		t.Errorf("expected 'Slide Two Content' in output, got: %q", got)
	}

	// Slide 1 must appear before Slide 2.
	idx1 := strings.Index(got, "Slide 1")
	idx2 := strings.Index(got, "Slide 2")
	if idx1 >= idx2 {
		t.Errorf("expected Slide 1 before Slide 2, got: %q", got)
	}
}

func TestExtractPPTX_notFound(t *testing.T) {
	_, err := extractPPTX("/nonexistent/path/file.pptx")
	if err == nil {
		t.Error("expected error for non-existent PPTX, got nil")
	}
}

// --- HTML ---

func TestExtractHTML(t *testing.T) {
	const src = `<!DOCTYPE html>
<html>
<head>
  <title>Test Page</title>
  <style>body { font-size: 16px; }</style>
  <script>console.log("skip me");</script>
</head>
<body>
  <h1>Main Heading</h1>
  <p>Hello <a href="https://example.com">Example</a> world.</p>
  <ul>
    <li>Item one</li>
    <li>Item two</li>
  </ul>
  <noscript>No JS</noscript>
</body>
</html>`

	path := t.TempDir() + "/test.html"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := extractHTML(path)
	if err != nil {
		t.Fatalf("extractHTML: %v", err)
	}

	// Heading and paragraph text must appear.
	if !strings.Contains(got, "Main Heading") {
		t.Errorf("expected 'Main Heading' in output, got: %q", got)
	}
	if !strings.Contains(got, "Hello") {
		t.Errorf("expected 'Hello' in output, got: %q", got)
	}
	// Link must be preserved as [text](href).
	if !strings.Contains(got, "[Example](https://example.com)") {
		t.Errorf("expected link '[Example](https://example.com)' in output, got: %q", got)
	}
	// List items must appear.
	if !strings.Contains(got, "Item one") {
		t.Errorf("expected 'Item one' in output, got: %q", got)
	}
	// Script and style content must NOT appear.
	if strings.Contains(got, "skip me") {
		t.Errorf("script content must be stripped, got: %q", got)
	}
	if strings.Contains(got, "font-size") {
		t.Errorf("style content must be stripped, got: %q", got)
	}
	// noscript content must be stripped.
	if strings.Contains(got, "No JS") {
		t.Errorf("noscript content must be stripped, got: %q", got)
	}
}

func TestExtractHTML_htmExtension(t *testing.T) {
	const src = `<html><body><p>HTM file</p></body></html>`
	path := t.TempDir() + "/test.htm"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := extractHTML(path)
	if err != nil {
		t.Fatalf("extractHTML .htm: %v", err)
	}
	if !strings.Contains(got, "HTM file") {
		t.Errorf("expected 'HTM file', got: %q", got)
	}
}

func TestExtractHTML_notFound(t *testing.T) {
	_, err := extractHTML("/nonexistent/path/file.html")
	if err == nil {
		t.Error("expected error for non-existent HTML, got nil")
	}
}
