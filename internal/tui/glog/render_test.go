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

	out := stripANSI(renderCommits(pr, commits, 0))

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

func TestStatusTailEmptyForNoHunks(t *testing.T) {
	c := coreglog.CommitRollup{Commit: gh.Commit{SHA: "merge12"}, Total: 0, Status: coreglog.StatusNone}
	if tail := statusTail(c); tail != "" {
		t.Errorf("expected empty tail for a no-hunk commit, got %q", tail)
	}
}
