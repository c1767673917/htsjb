package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateOrderFilePathRejectsTraversal(t *testing.T) {
	t.Parallel()

	service := New(t.TempDir())
	if err := service.EnsureLayout(); err != nil {
		t.Fatalf("ensure layout: %v", err)
	}

	cases := []string{
		"../evil.txt",
		"..\\evil.txt",
		"/absolute/path.txt",
		"C:\\windows\\system32\\evil.txt",
		"nested/path.txt",
		"nested\\path.txt",
		"evil.txt\x00.jpg",
		"%2e%2e%2fevil.txt",
	}
	for _, filename := range cases {
		if _, err := service.ValidateOrderFilePath(2021, "RX2101-22926", filename); err == nil {
			t.Fatalf("expected traversal rejection for %q", filename)
		}
	}
}

func TestOrderDirRejectsUnsafeOrderNo(t *testing.T) {
	t.Parallel()

	service := New(t.TempDir())
	if err := service.EnsureLayout(); err != nil {
		t.Fatalf("ensure layout: %v", err)
	}

	cases := []string{
		"../evil",
		"..\\evil",
		"%2e%2e",
		".hidden",
		"/absolute",
		"evil\x00",
		"line\nbreak",
	}
	for _, orderNo := range cases {
		if _, err := service.OrderDir(2021, orderNo); err == nil {
			t.Fatalf("expected unsafe orderNo %q to be rejected", orderNo)
		}
	}
}

func TestValidateOrderFilePathRejectsSymlink(t *testing.T) {
	t.Parallel()

	service := New(t.TempDir())
	if err := service.EnsureLayout(); err != nil {
		t.Fatalf("ensure layout: %v", err)
	}

	orderDir, err := service.OrderDir(2021, "RX2101-22926")
	if err != nil {
		t.Fatalf("order dir: %v", err)
	}
	if err := os.MkdirAll(orderDir, 0o700); err != nil {
		t.Fatalf("mkdir order dir: %v", err)
	}

	target := filepath.Join(t.TempDir(), "target.txt")
	if err := os.WriteFile(target, []byte("payload"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	link := filepath.Join(orderDir, "link.jpg")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink target: %v", err)
	}

	if _, err := service.ValidateOrderFilePath(2021, "RX2101-22926", "link.jpg"); err == nil {
		t.Fatalf("expected symlink rejection")
	}
}

func TestValidateOrderFilePathRejectsUnsafeOrderNo(t *testing.T) {
	t.Parallel()

	service := New(t.TempDir())
	if err := service.EnsureLayout(); err != nil {
		t.Fatalf("ensure layout: %v", err)
	}

	cases := []string{
		"..\\evil",
		"%2e%2e",
		".hidden",
		"C:\\evil",
	}
	for _, orderNo := range cases {
		if _, err := service.ValidateOrderFilePath(2021, orderNo, "file.jpg"); err == nil {
			t.Fatalf("expected unsafe orderNo %q to be rejected", orderNo)
		}
	}
}
