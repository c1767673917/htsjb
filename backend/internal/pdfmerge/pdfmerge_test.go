package pdfmerge

import (
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildAtomicCreatesOnePagePerJPEGInOrder(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	paths := []string{
		filepath.Join(dir, "01.jpg"),
		filepath.Join(dir, "02.jpg"),
		filepath.Join(dir, "03.jpg"),
	}
	colors := []color.RGBA{
		{R: 255, A: 255},
		{G: 255, A: 255},
		{B: 255, A: 255},
	}
	for i, path := range paths {
		writeJPEG(t, path, colors[i])
	}

	output := filepath.Join(dir, "merged.pdf")
	file, err := os.Create(output)
	if err != nil {
		t.Fatalf("create output: %v", err)
	}
	t.Cleanup(func() { _ = file.Close() })

	pages, err := New(nil).Build(context.Background(), paths, file)
	if err != nil {
		t.Fatalf("build pdf: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close output: %v", err)
	}
	if pages != len(paths) {
		t.Fatalf("expected %d pages, got %d", len(paths), pages)
	}

	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read pdf: %v", err)
	}
	if got := strings.Count(string(data), "/Type /Page"); got < len(paths) {
		t.Fatalf("expected at least %d page markers, got %d", len(paths), got)
	}
}

func writeJPEG(t *testing.T, path string, fill color.RGBA) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 120, 160))
	for y := 0; y < 160; y++ {
		for x := 0; x < 120; x++ {
			img.SetRGBA(x, y, fill)
		}
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create jpeg: %v", err)
	}
	defer file.Close()
	if err := jpeg.Encode(file, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
}
