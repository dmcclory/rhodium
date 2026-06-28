// Package glog computes the per-commit review-status overlay for the glog
// ("commit log with review overlay") view. The core is a pure function that
// intersects each commit's hunks with the PR's marked hunk hashes — see the
// design note (~/projects/review_tool/glog-and-stack-index-design.md §3a).
package glog

import (
	"rhodium/internal/diff"
	"rhodium/internal/gh"
)

// Status is a commit's rolled-up review state, derived from how many of the
// hunks it introduced have been marked.
type Status int

const (
	StatusNone    Status = iota // no markable hunks marked (renders [ ])
	StatusPartial               // some but not all marked (renders [~])
	StatusAll                   // every markable hunk marked (renders [✓])
)

// CommitRollup pairs a commit with its review-status overlay.
type CommitRollup struct {
	Commit gh.Commit
	Marked int // markable hunks of this commit that are marked
	Total  int // markable hunks this commit introduced
	Notes  int // notes attributed to this commit (populated by a later pass)
	Status Status
}

// Rollup computes per-commit review status by intersecting each commit's
// hunks with the PR's marked hunk hashes.
//
// Marks are content-addressed: HashHunkBody (which diff.ParseHunks already
// applies) hashes only a hunk's +/- lines, so a commit's hunk and the
// PR-level hunk carrying the same change share a hash. A commit's hunk counts
// as reviewed when its hash appears in the marks for that path.
//
//   - commitFiles maps a commit SHA to the files it introduced
//     (gh.FetchCommitFiles).
//   - marksByPath maps a file path to its marked hunk hashes with counts
//     (brain.HunkMarks), per path.
//
// This is the Tier-1 (exact hash-intersection) rollup: precise when a hunk is
// introduced by one commit and survives unchanged to head, and biased toward
// "looks less reviewed" when a later commit churns the same lines.
func Rollup(commits []gh.Commit, commitFiles map[string][]gh.FileChange, marksByPath map[string]map[string]int) []CommitRollup {
	out := make([]CommitRollup, 0, len(commits))
	for _, c := range commits {
		var marked, total int
		for _, f := range commitFiles[c.SHA] {
			for _, h := range diff.ParseHunks(f.Patch) {
				if !h.IsMarkable() {
					continue
				}
				total++
				if marksByPath[f.Path][h.Hash] > 0 {
					marked++
				}
			}
		}
		out = append(out, CommitRollup{
			Commit: c,
			Marked: marked,
			Total:  total,
			Status: statusFor(marked, total),
		})
	}
	return out
}

func statusFor(marked, total int) Status {
	switch {
	case total == 0 || marked == 0:
		return StatusNone
	case marked >= total:
		return StatusAll
	default:
		return StatusPartial
	}
}
