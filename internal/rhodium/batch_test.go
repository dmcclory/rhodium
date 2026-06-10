package rhodium

import (
	"path/filepath"
	"rhodium/internal/brain"
	"rhodium/internal/gh"
	"rhodium/internal/tui/prs"
	"rhodium/internal/tui/todo"
	"testing"
)

func testApp(t *testing.T) *app {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("RHODIUM_BRAIN", filepath.Join(dir, "brain.db"))

	b, err := brain.LoadBrain()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })

	return &app{
		cfg:   &Config{Repos: []string{"acme/web"}},
		brain: b,
		cache: newCache(),
		prs:   prs.New(),
		todo:  todo.New(),
	}
}

func TestOnBatchFilesLoadedCachesResults(t *testing.T) {
	a := testApp(t)

	pr1 := gh.PR{Repo: "acme/web", Number: 1, HeadSHA: "abc", BaseSHA: "def"}
	pr2 := gh.PR{Repo: "acme/web", Number: 2, HeadSHA: "ghi", BaseSHA: "jkl"}

	files1 := []gh.FileChange{{Path: "main.go", Additions: 10, Deletions: 2}}
	files2 := []gh.FileChange{{Path: "util.go", Additions: 5, Deletions: 0}}

	msg := batchFilesLoadedMsg{
		results: []batchFileResult{
			{pr: pr1, files: files1, err: nil},
			{pr: pr2, files: files2, err: nil},
			{pr: gh.PR{Repo: "acme/web", Number: 3}, files: nil, err: &testErr{"fetch failed"}},
		},
	}

	_ = a.onBatchFilesLoaded(msg)

	// Should have cached file lists for the two successful PRs.
	if len(a.cache.prFiles) != 2 {
		t.Errorf("prFiles has %d entries, want 2", len(a.cache.prFiles))
	}

	k1 := brain.PRKey(pr1.Repo, pr1.Number)
	if got, ok := a.cache.prFiles[k1]; !ok || len(got) != 1 {
		t.Errorf("PR #1 files: got %v, want 1 file", got)
	}

	k2 := brain.PRKey(pr2.Repo, pr2.Number)
	if got, ok := a.cache.prFiles[k2]; !ok || len(got) != 1 {
		t.Errorf("PR #2 files: got %v, want 1 file", got)
	}

	// Error result should not have been cached.
	k3 := brain.PRKey("acme/web", 3)
	if _, ok := a.cache.prFiles[k3]; ok {
		t.Error("prFiles should not have entry for failed PR #3")
	}
}

func TestOnBatchFilesLoadedSkipsScrutinized(t *testing.T) {
	a := testApp(t)

	pr := gh.PR{Repo: "acme/web", Number: 1, HeadSHA: "abc", BaseSHA: "def"}
	a.brain.SetScrutiny(pr.Repo, pr.Number, true)

	msg := batchFilesLoadedMsg{
		results: []batchFileResult{
			{pr: pr, files: []gh.FileChange{{Path: "main.go"}}, err: nil},
		},
	}

	cmd := a.onBatchFilesLoaded(msg)

	// Scrutinized PR should not trigger auto-advance.
	if cmd != nil {
		t.Error("expected nil cmd for scrutinized PR, got cmds")
	}

	// File list should still be cached.
	k := brain.PRKey(pr.Repo, pr.Number)
	if _, ok := a.cache.prFiles[k]; !ok {
		t.Error("prFiles should have entry for scrutinized PR")
	}
}

func TestOnBatchFilesLoadedEmptyResults(t *testing.T) {
	a := testApp(t)

	msg := batchFilesLoadedMsg{results: []batchFileResult{}}
	cmd := a.onBatchFilesLoaded(msg)

	if len(a.cache.prFiles) != 0 {
		t.Errorf("prFiles has %d entries, want 0", len(a.cache.prFiles))
	}
	if cmd != nil {
		t.Error("expected nil cmd for empty results")
	}
}

func TestOnBatchCommentsLoadedCachesResults(t *testing.T) {
	a := testApp(t)

	comments1 := []gh.Comment{{GHID: 1, Body: "nice!"}}
	comments2 := []gh.Comment{{GHID: 2, Body: "LGTM"}}

	msg := batchCommentsLoadedMsg{
		results: []batchCommentResult{
			{repo: "acme/web", prNum: 1, comments: comments1, err: nil},
			{repo: "acme/web", prNum: 2, comments: comments2, err: nil},
			{repo: "acme/web", prNum: 3, comments: nil, err: &testErr{"rate limited"}},
		},
	}

	cmd := a.onBatchCommentsLoaded(msg)

	// Should have cached comments for the two successful PRs.
	if len(a.cache.prComments) != 2 {
		t.Errorf("prComments has %d entries, want 2", len(a.cache.prComments))
	}

	k1 := brain.PRKey("acme/web", 1)
	if len(a.cache.prComments[k1]) != 1 {
		t.Errorf("PR #1 comments: got %d, want 1", len(a.cache.prComments[k1]))
	}

	k2 := brain.PRKey("acme/web", 2)
	if len(a.cache.prComments[k2]) != 1 {
		t.Errorf("PR #2 comments: got %d, want 1", len(a.cache.prComments[k2]))
	}

	// Error result should not have been cached.
	k3 := brain.PRKey("acme/web", 3)
	if _, ok := a.cache.prComments[k3]; ok {
		t.Error("prComments should not have entry for failed PR #3")
	}

	// Handler should return nil.
	if cmd != nil {
		t.Error("expected nil cmd, got non-nil")
	}
}

func TestOnBatchCommentsLoadedEmptyResults(t *testing.T) {
	a := testApp(t)

	msg := batchCommentsLoadedMsg{results: []batchCommentResult{}}
	cmd := a.onBatchCommentsLoaded(msg)

	if len(a.cache.prComments) != 0 {
		t.Errorf("prComments has %d entries, want 0", len(a.cache.prComments))
	}
	if cmd != nil {
		t.Error("expected nil cmd for empty results")
	}
}

// testErr is a simple error implementation for test fixtures.
type testErr struct{ msg string }

func (e *testErr) Error() string { return e.msg }
