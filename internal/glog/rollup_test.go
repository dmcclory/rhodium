package glog

import (
	"testing"

	"rhodium/internal/diff"
	"rhodium/internal/gh"
)

// hunkHash returns the hash diff.ParseHunks assigns to the (single) hunk in
// patch, so tests can seed marks that match a commit's hunks.
func hunkHash(t *testing.T, patch string) string {
	t.Helper()
	hunks := diff.ParseHunks(patch)
	if len(hunks) != 1 {
		t.Fatalf("expected 1 hunk in patch, got %d", len(hunks))
	}
	return hunks[0].Hash
}

const (
	fooPatch = "@@ -0,0 +1,2 @@\n+func foo() {\n+}\n"
	barPatch = "@@ -0,0 +1,2 @@\n+func bar() {\n+}\n"
)

func TestRollupAllMarked(t *testing.T) {
	commits := []gh.Commit{{SHA: "c1", Title: "add foo"}}
	commitFiles := map[string][]gh.FileChange{
		"c1": {{Path: "foo.go", Patch: fooPatch}},
	}
	marks := map[string]map[string]int{
		"foo.go": {hunkHash(t, fooPatch): 1},
	}

	got := Rollup(commits, commitFiles, marks)
	if len(got) != 1 {
		t.Fatalf("expected 1 rollup, got %d", len(got))
	}
	if got[0].Status != StatusAll {
		t.Errorf("status = %v, want StatusAll", got[0].Status)
	}
	if got[0].Marked != 1 || got[0].Total != 1 {
		t.Errorf("marked/total = %d/%d, want 1/1", got[0].Marked, got[0].Total)
	}
}

func TestRollupPartial(t *testing.T) {
	// One commit touches two files; only one hunk is marked.
	commits := []gh.Commit{{SHA: "c1"}}
	commitFiles := map[string][]gh.FileChange{
		"c1": {
			{Path: "foo.go", Patch: fooPatch},
			{Path: "bar.go", Patch: barPatch},
		},
	}
	marks := map[string]map[string]int{
		"foo.go": {hunkHash(t, fooPatch): 1},
	}

	got := Rollup(commits, commitFiles, marks)
	if got[0].Status != StatusPartial {
		t.Errorf("status = %v, want StatusPartial", got[0].Status)
	}
	if got[0].Marked != 1 || got[0].Total != 2 {
		t.Errorf("marked/total = %d/%d, want 1/2", got[0].Marked, got[0].Total)
	}
}

func TestRollupNoneMarked(t *testing.T) {
	commits := []gh.Commit{{SHA: "c1"}}
	commitFiles := map[string][]gh.FileChange{
		"c1": {{Path: "foo.go", Patch: fooPatch}},
	}
	// Marks for a different hash → no intersection.
	marks := map[string]map[string]int{
		"foo.go": {"deadbeefdeadbeef": 1},
	}

	got := Rollup(commits, commitFiles, marks)
	if got[0].Status != StatusNone {
		t.Errorf("status = %v, want StatusNone", got[0].Status)
	}
	if got[0].Marked != 0 || got[0].Total != 1 {
		t.Errorf("marked/total = %d/%d, want 0/1", got[0].Marked, got[0].Total)
	}
}

func TestRollupNoHunks(t *testing.T) {
	// Merge commit with empty patches → no markable hunks → StatusNone.
	commits := []gh.Commit{{SHA: "merge"}}
	commitFiles := map[string][]gh.FileChange{
		"merge": {{Path: "foo.go", Patch: ""}},
	}

	got := Rollup(commits, commitFiles, nil)
	if got[0].Status != StatusNone {
		t.Errorf("status = %v, want StatusNone", got[0].Status)
	}
	if got[0].Total != 0 {
		t.Errorf("total = %d, want 0", got[0].Total)
	}
}

func TestRollupMultipleCommitsIndependent(t *testing.T) {
	commits := []gh.Commit{{SHA: "c1"}, {SHA: "c2"}}
	commitFiles := map[string][]gh.FileChange{
		"c1": {{Path: "foo.go", Patch: fooPatch}},
		"c2": {{Path: "bar.go", Patch: barPatch}},
	}
	// Only c1's hunk is marked.
	marks := map[string]map[string]int{
		"foo.go": {hunkHash(t, fooPatch): 1},
	}

	got := Rollup(commits, commitFiles, marks)
	if got[0].Status != StatusAll {
		t.Errorf("c1 status = %v, want StatusAll", got[0].Status)
	}
	if got[1].Status != StatusNone {
		t.Errorf("c2 status = %v, want StatusNone", got[1].Status)
	}
}
