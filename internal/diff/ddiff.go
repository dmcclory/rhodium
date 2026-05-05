package diff

import (
	"fmt"
	"strings"
)

// StorySummary returns a one-line narrative for a segment showing what the
// rebase did between F1 and F2.  Format:
//
//	"story: f1 had 12 lines; rebase dropped 4, added 8 → f2 has 16"
//
// Returns an empty string for segments with no F1 content (pure additions)
// or where F1 == F2 (no rebase delta).
func StorySummary(seg Segment) string {
	f1Lines := splitLinesForSeg(seg.F1)
	f2Lines := splitLinesForSeg(seg.F2)

	f1Count := len(f1Lines)
	f2Count := len(f2Lines)

	if f1Count == 0 && f2Count == 0 {
		return "" // empty everywhere
	}
	if f1Count == 0 {
		// Pure addition — nothing to "drop".
		return fmt.Sprintf("story: +%d new lines → f2 has %d", f2Count, f2Count)
	}
	if f2Count == 0 {
		// Pure deletion.
		return fmt.Sprintf("story: f1 had %d lines; rebase dropped all", f1Count)
	}
	if seg.F1 == seg.F2 {
		return "" // identical — no rebase story to tell
	}

	// Diff F1 vs F2 to get dropped/added counts.
	ops := buildDiffOps(f1Lines, f2Lines, PatienceMatches(f1Lines, f2Lines))
	var dropped, added int
	for _, op := range ops {
		switch op.kind {
		case '-':
			dropped++
		case '+':
			added++
		}
	}

	if dropped == 0 && added == 0 {
		return "" // no changes
	}

	parts := []string{fmt.Sprintf("f1 had %d lines", f1Count)}
	if dropped > 0 {
		parts = append(parts, fmt.Sprintf("rebase dropped %d", dropped))
	}
	if added > 0 {
		parts = append(parts, fmt.Sprintf("added %d", added))
	}
	return "story: " + strings.Join(parts, "; ") + fmt.Sprintf(" → f2 has %d", f2Count)
}
