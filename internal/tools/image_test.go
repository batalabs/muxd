package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractImagePaths(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "test.png")
	os.WriteFile(imgPath, []byte("fake png data"), 0o644)

	tests := []struct {
		name      string
		input     string
		wantPaths int
		wantText  string
	}{
		{
			name:      "no images",
			input:     "hello world",
			wantPaths: 0,
			wantText:  "hello world",
		},
		{
			name:      "single image path",
			input:     "describe this " + imgPath,
			wantPaths: 1,
			wantText:  "describe this",
		},
		{
			name:      "image path only",
			input:     imgPath,
			wantPaths: 1,
			wantText:  "",
		},
		{
			name:      "nonexistent image ignored",
			input:     "/tmp/nonexistent_abc123_xyz.png",
			wantPaths: 0,
			wantText:  "/tmp/nonexistent_abc123_xyz.png",
		},
		{
			name:      "text before and after image",
			input:     "look at " + imgPath + " please",
			wantPaths: 1,
			wantText:  "look at please",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paths, text := ExtractImagePaths(tt.input)
			if len(paths) != tt.wantPaths {
				t.Errorf("expected %d paths, got %d: %v", tt.wantPaths, len(paths), paths)
			}
			if text != tt.wantText {
				t.Errorf("expected text %q, got %q", tt.wantText, text)
			}
		})
	}
}

func TestExtractImagePaths_multipleImages(t *testing.T) {
	dir := t.TempDir()
	img1 := filepath.Join(dir, "a.png")
	img2 := filepath.Join(dir, "b.jpg")
	os.WriteFile(img1, []byte("png"), 0o644)
	os.WriteFile(img2, []byte("jpg"), 0o644)

	paths, text := ExtractImagePaths(img1 + " compare with " + img2)
	if len(paths) != 2 {
		t.Fatalf("expected 2 paths, got %d: %v", len(paths), paths)
	}
	if text != "compare with" {
		t.Errorf("expected remaining text %q, got %q", "compare with", text)
	}
}

func TestMediaTypeFromExt(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"photo.png", "image/png"},
		{"photo.PNG", "image/png"},
		{"photo.jpg", "image/jpeg"},
		{"photo.jpeg", "image/jpeg"},
		{"photo.gif", "image/gif"},
		{"photo.webp", "image/webp"},
		{"photo.bmp", "image/bmp"},
		{"photo.txt", ""},
		{"photo.go", ""},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := MediaTypeFromExt(tt.path)
			if got != tt.want {
				t.Errorf("MediaTypeFromExt(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}
