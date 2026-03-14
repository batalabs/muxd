package docread

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestCanExtract(t *testing.T) {
	tests := []struct {
		ext  string
		want bool
	}{
		{".pdf", true},
		{".docx", true},
		{".xlsx", true},
		{".pptx", true},
		{".html", true},
		{".htm", true},
		{".csv", true},
		{".json", true},
		{".xml", true},
		{".go", false},
		{".txt", false},
		{".png", false},
		{".js", false},
		{"", false},
	}
	for _, tc := range tests {
		t.Run(tc.ext, func(t *testing.T) {
			got := CanExtract(tc.ext)
			if got != tc.want {
				t.Errorf("CanExtract(%q) = %v, want %v", tc.ext, got, tc.want)
			}
		})
	}
}

func TestExtract_unsupportedFormat(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "test*.xyz")
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	_, err = Extract(f.Name())
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Errorf("Extract on .xyz: got %v, want ErrUnsupportedFormat", err)
	}
}

func TestExtractCSV(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.csv"
	content := "name,age,city\nAlice,30,NYC\nBob,25,LA\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Extract(path)
	if err != nil {
		t.Fatalf("Extract CSV: %v", err)
	}

	// Each row should be tab-separated.
	if !strings.Contains(got, "name\tage\tcity") {
		t.Errorf("expected tab-separated header, got: %q", got)
	}
	if !strings.Contains(got, "Alice\t30\tNYC") {
		t.Errorf("expected tab-separated row, got: %q", got)
	}
	if !strings.Contains(got, "Bob\t25\tLA") {
		t.Errorf("expected tab-separated row, got: %q", got)
	}
}

func TestExtractJSON(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.json"
	// Compact JSON — should be pretty-printed on output.
	raw := `{"name":"Alice","scores":[1,2,3]}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Extract(path)
	if err != nil {
		t.Fatalf("Extract JSON: %v", err)
	}

	// Must be valid JSON still.
	var v any
	if err := json.Unmarshal([]byte(got), &v); err != nil {
		t.Errorf("output is not valid JSON: %v\noutput: %q", err, got)
	}

	// Must be pretty-printed (contain newlines).
	if !strings.Contains(got, "\n") {
		t.Errorf("expected pretty-printed JSON with newlines, got: %q", got)
	}
}

func TestExtractXML(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.xml"
	content := `<?xml version="1.0"?><root><item>Hello</item><item>World</item></root>`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Extract(path)
	if err != nil {
		t.Fatalf("Extract XML: %v", err)
	}

	// Should contain the text nodes, not the tags.
	if !strings.Contains(got, "Hello") {
		t.Errorf("expected 'Hello' in XML output, got: %q", got)
	}
	if !strings.Contains(got, "World") {
		t.Errorf("expected 'World' in XML output, got: %q", got)
	}
	// Should not contain raw XML tags.
	if strings.Contains(got, "<item>") {
		t.Errorf("expected tags stripped from XML output, got: %q", got)
	}
}

func TestExtract_fileTooLarge(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/big.json"

	// Write a file larger than maxFileSize (10 MB).
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	// Seek to just past 10 MB and write a single byte to create a sparse file.
	if _, err := f.WriteAt([]byte{0}, maxFileSize+1); err != nil {
		f.Close()
		t.Skipf("cannot create large sparse file on this OS: %v", err)
	}
	f.Close()

	_, err = Extract(path)
	if err == nil {
		t.Error("expected error for file > 10 MB, got nil")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("expected 'too large' in error, got: %v", err)
	}
}
