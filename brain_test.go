package main

import (
	"path/filepath"
	"testing"
)

const samplePatch = `@@ -1,3 +1,4 @@
 context
-old line
+new line
+extra
@@ -10,2 +11,2 @@
 more ctx
-gone
+added
`

func TestBrainHunkMarks(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RHODIUM_BRAIN", filepath.Join(dir, "brain.json"))

	b, err := LoadBrain()
	if err != nil {
		t.Fatal(err)
	}

	fc := FileChange{Path: "src/main.go", Patch: samplePatch}
	hunks := parseHunks(samplePatch)
	if len(hunks) != 2 {
		t.Fatalf("expected 2 hunks, got %d", len(hunks))
	}

	if s := b.Status("acme/web", 42, fc); s != StatusUnseen {
		t.Errorf("fresh: got %v, want StatusUnseen", s)
	}

	// Mark only the first hunk → partial.
	marks := map[string]bool{hunks[0].Hash: true}
	if err := b.SetHunkMarks("acme/web", 42, fc.Path, marks); err != nil {
		t.Fatal(err)
	}
	if s := b.Status("acme/web", 42, fc); s != StatusPartial {
		t.Errorf("one of two: got %v, want StatusPartial", s)
	}

	// Mark both → seen.
	marks[hunks[1].Hash] = true
	if err := b.SetHunkMarks("acme/web", 42, fc.Path, marks); err != nil {
		t.Fatal(err)
	}
	if s := b.Status("acme/web", 42, fc); s != StatusSeen {
		t.Errorf("both: got %v, want StatusSeen", s)
	}

	// Stability: a patch with the same hunks in a different context (shifted line numbers)
	// should still be Seen because we hash +/- only.
	shifted := `@@ -100,3 +100,4 @@
 context
-old line
+new line
+extra
@@ -200,2 +201,2 @@
 more ctx
-gone
+added
`
	if s := b.Status("acme/web", 42, FileChange{Path: "src/main.go", Patch: shifted}); s != StatusSeen {
		t.Errorf("shifted line numbers: got %v, want StatusSeen", s)
	}

	// Reload persistence.
	b2, err := LoadBrain()
	if err != nil {
		t.Fatal(err)
	}
	if s := b2.Status("acme/web", 42, fc); s != StatusSeen {
		t.Errorf("after reload: got %v, want StatusSeen", s)
	}
}
