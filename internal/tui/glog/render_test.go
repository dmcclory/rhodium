package glog

import (
	"regexp"
	"strings"
	"testing"

	"rhodium/internal/gh"
	coreglog "rhodium/internal/glog"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

func TestRenderCommitsBadgesAndSummary(t *testing.T) {
	pr := &gh.PR{Repo: "octo/web", Number: 42, Title: "Refactor auth"}
	commits := []coreglog.CommitRollup{
		{Commit: gh.Commit{SHA: "a1b9f2cdeadbeef", Title: "extract parser", Author: "tj"}, Marked: 2, Total: 2, Status: coreglog.StatusAll},
		{Commit: gh.Commit{SHA: "9ad3e05beefcafe", Title: "wire it up", Author: "dan"}, Marked: 1, Total: 2, Status: coreglog.StatusPartial},
		{Commit: gh.Commit{SHA: "f02bb71facefeed", Title: "fix race", Author: "dan"}, Marked: 0, Total: 1, Status: coreglog.StatusNone},
	}

	out := stripANSI(renderCommits(pr, commits, 0, nil))

	// Header with repo/number and commit count.
	if !strings.Contains(out, "octo/web#42") || !strings.Contains(out, "3 commits") {
		t.Errorf("missing header:\n%s", out)
	}
	// Short SHAs (7 chars) and titles.
	for _, want := range []string{"a1b9f2c", "extract parser", "9ad3e05", "wire it up", "f02bb71", "fix race"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// Badges and tails per status.
	for _, want := range []string{"[✓]", "✔ reviewed", "[~]", "◐ 1/2 hunks", "[ ]", "○ unreviewed"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// Summary: 1 fully-reviewed commit of 3, 3 marked hunks of 5.
	if !strings.Contains(out, "reviewed 1/3 commits · 3/5 hunks") {
		t.Errorf("missing/incorrect summary:\n%s", out)
	}
}

func TestRenderExpandedShowsFilesAndHunks(t *testing.T) {
	commits := []coreglog.CommitRollup{
		{
			Commit: gh.Commit{SHA: "a1b9f2cdeadbeef", Title: "extract parser"},
			Files: []coreglog.FileRollup{
				{
					Path: "auth/middleware.go", Additions: 9, Deletions: 22, Marked: 1, Total: 2,
					Hunks: []coreglog.HunkStatus{
						{Header: "@@ -1,3 +1,4 @@ func wireRotation() {", Marked: true},
						{Header: "@@ -10,2 +11,3 @@ func handleExpiry() {", Marked: false},
					},
				},
			},
			Marked: 1, Total: 2, Status: coreglog.StatusPartial,
		},
	}

	collapsed := stripANSI(renderCommits(nil, commits, 0, nil))
	if strings.Contains(collapsed, "wireRotation") {
		t.Errorf("collapsed view should not show hunk detail:\n%s", collapsed)
	}

	expanded := stripANSI(renderCommits(nil, commits, 0, map[int]bool{0: true}))
	for _, want := range []string{"auth/middleware.go", "+9 −22", "wireRotation", "handleExpiry", "◐ 1/2"} {
		if !strings.Contains(expanded, want) {
			t.Errorf("expanded view missing %q in:\n%s", want, expanded)
		}
	}
}

func TestSetCommitsDefaultsToExpanded(t *testing.T) {
	m := New()
	commits := []coreglog.CommitRollup{
		{Commit: gh.Commit{SHA: "c1"}},
		{Commit: gh.Commit{SHA: "c2"}},
	}
	m.SetCommits(nil, commits)
	for i := range commits {
		if !m.expanded[i] {
			t.Errorf("commit %d should default to expanded", i)
		}
	}
}

func TestStatusTailEmptyForNoHunks(t *testing.T) {
	c := coreglog.CommitRollup{Commit: gh.Commit{SHA: "merge12"}, Total: 0, Status: coreglog.StatusNone}
	if tail := statusTail(c); tail != "" {
		t.Errorf("expected empty tail for a no-hunk commit, got %q", tail)
	}
}
