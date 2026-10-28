package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveFrontendDir(t *testing.T) {
	root := t.TempDir()
	backend := filepath.Join(root, "backend")
	dist := filepath.Join(root, "frontend", "dist")
	for _, dir := range []string{backend, dist} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(backend)

	if got := resolveFrontendDir(""); got != "" {
		t.Fatalf("without index.html got %q, want empty", got)
	}

	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := resolveFrontendDir(""); got != "../frontend/dist" {
		t.Fatalf("auto-detect got %q, want ../frontend/dist", got)
	}

	// A configured dir is used alone, never falling back to auto-detect.
	if got := resolveFrontendDir(filepath.Join(root, "missing")); got != "" {
		t.Fatalf("missing configured dir got %q, want empty", got)
	}
	if got := resolveFrontendDir(dist); got != dist {
		t.Fatalf("configured dir got %q, want %q", got, dist)
	}
}
