package chunkdiscoverer

import (
	"context"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

func TestStandaloneDiscovererUsesEmbeddedEngine(t *testing.T) {
	if _, err := exec.LookPath("bun"); err != nil {
		t.Skip("bun is not installed")
	}
	discoverer, err := NewStandaloneDiscoverer()
	if err != nil {
		t.Fatal(err)
	}
	defer discoverer.Close()

	chunks, err := discoverer.Discover(context.Background(), filepath.Join("..", "..", "..", "pkg", "chunk-discoverer", "tests", "files", "2.js"), 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"normal.js", "preloaded2.js", "inner.js"} {
		if !slices.Contains(chunks, expected) {
			t.Fatalf("expected %q in %#v", expected, chunks)
		}
	}
}
