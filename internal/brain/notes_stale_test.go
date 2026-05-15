package brain

import (
	"errors"
	"path/filepath"
	"testing"

	"rhodium/internal/gh"
)

// TestResolveStaleNotes_PreservesOnTransientError verifies that a transient
// `gh` failure (auth lapse, network glitch, rate limit) does NOT bulk-resolve
// every active note on a file. The previous behavior collapsed all errors to
// "" content, which ResolveStaleNotes interpreted as "file gone" and resolved
// every note as stale.
func TestResolveStaleNotes_PreservesOnTransientError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RHODIUM_BRAIN", filepath.Join(dir, "brain.db"))

	b, err := LoadBrain()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	// Seed two active notes on the same file.
	if err := b.SaveNote("acme/web", 42, "a.go", 10, "h1", "first", ""); err != nil {
		t.Fatal(err)
	}
	if err := b.SaveNote("acme/web", 42, "a.go", 20, "h2", "second", ""); err != nil {
		t.Fatal(err)
	}

	// Stub the file fetcher to return a transient error (NOT ErrFileNotFound).
	orig := gh.FetchFileAtRefFn
	t.Cleanup(func() { gh.FetchFileAtRefFn = orig })
	gh.FetchFileAtRefFn = func(repo, path, ref string) (string, error) {
		return "", errors.New("gh: network unreachable")
	}

	n, err := b.ResolveStaleNotes("acme/web", 42, "deadbeef")
	if err != nil {
		t.Fatalf("ResolveStaleNotes returned unexpected error: %v", err)
	}
	if n != 0 {
		t.Fatalf("ResolveStaleNotes resolved %d notes; want 0 (transient error must not bulk-resolve)", n)
	}

	active := b.NotesForPR("acme/web", 42, NotesActive)
	if len(active) != 2 {
		t.Fatalf("got %d active notes after transient fetch failure; want 2", len(active))
	}
}

// TestResolveStaleNotes_ResolvesOnFileDeleted confirms the legitimate
// file-missing path still resolves notes — when FetchFileAtRef returns
// ErrFileNotFound, notes on that file are stale.
func TestResolveStaleNotes_ResolvesOnFileDeleted(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RHODIUM_BRAIN", filepath.Join(dir, "brain.db"))

	b, err := LoadBrain()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	if err := b.SaveNote("acme/web", 42, "gone.go", 10, "h1", "note", ""); err != nil {
		t.Fatal(err)
	}

	orig := gh.FetchFileAtRefFn
	t.Cleanup(func() { gh.FetchFileAtRefFn = orig })
	gh.FetchFileAtRefFn = func(repo, path, ref string) (string, error) {
		return "", gh.ErrFileNotFound
	}

	n, err := b.ResolveStaleNotes("acme/web", 42, "deadbeef")
	if err != nil {
		t.Fatalf("ResolveStaleNotes returned unexpected error: %v", err)
	}
	if n != 1 {
		t.Fatalf("ResolveStaleNotes resolved %d notes; want 1 (file-deleted should resolve)", n)
	}
}
