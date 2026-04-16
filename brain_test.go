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
	t.Setenv("RHODIUM_BRAIN", filepath.Join(dir, "brain.db"))

	b, err := LoadBrain()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

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
	defer b2.Close()
	if s := b2.Status("acme/web", 42, fc); s != StatusSeen {
		t.Errorf("after reload: got %v, want StatusSeen", s)
	}
}

func TestBrainPRCache(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RHODIUM_BRAIN", filepath.Join(dir, "brain.db"))

	b, err := LoadBrain()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	if prs := b.CachedPRs(); len(prs) != 0 {
		t.Errorf("fresh cache: got %d, want 0", len(prs))
	}

	want := []PR{
		{Repo: "cli/cli", Number: 1, Title: "fix thing", Author: "alice", HeadSHA: "abc123"},
		{Repo: "charm/bubbletea", Number: 2, Title: "add feature", Author: "bob", HeadSHA: "def456"},
	}
	if err := b.SetPRCache(want); err != nil {
		t.Fatal(err)
	}

	got := b.CachedPRs()
	if len(got) != len(want) {
		t.Fatalf("cache: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Repo != want[i].Repo || got[i].Number != want[i].Number {
			t.Errorf("cache[%d]: got %s#%d, want %s#%d", i, got[i].Repo, got[i].Number, want[i].Repo, want[i].Number)
		}
	}

	// Reload persists.
	b2, err := LoadBrain()
	if err != nil {
		t.Fatal(err)
	}
	defer b2.Close()
	got2 := b2.CachedPRs()
	if len(got2) != len(want) {
		t.Fatalf("reload cache: got %d, want %d", len(got2), len(want))
	}
}
