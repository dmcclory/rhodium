package diff

import (
	"fmt"
	"strings"
)

// DDiffKind is a classification label for a line in the feature ddiff
// (F1 vs F2, anchored by B1/B2 context).
type DDiffKind int

const (
	DDiffKept       DDiffKind = iota // present in both F1 and F2
	DDiffDropped                     // in F1, missing from F2
	DDiffAdded                       // in F2, missing from F1
	DDiffAbsorbed                    // in F1→F2 removed, but already in B2 (base absorbed it)
	DDiffPropagated                  // in F2 but not F1, present in B2 (base change propagated)
)

func (k DDiffKind) String() string {
	switch k {
	case DDiffKept:
		return "kept"
	case DDiffDropped:
		return "dropped"
	case DDiffAdded:
		return "added"
	case DDiffAbsorbed:
		return "absorbed"
	case DDiffPropagated:
		return "propagated"
	default:
		return "?"
	}
}

// DDiffLine is one line of the feature ddiff with its classification.
type DDiffLine struct {
	Kind DDiffKind
	Text string
}

// FeatureDDiff computes the classified diff between F1 and F2 for a
// segment, using B1/B2 context to label lines as absorbed/propagated
// vs. dropped/added.  Returns nil if F1 == F2 (no rebase delta to show).
func FeatureDDiff(seg Segment) []DDiffLine {
	f1Lines := splitLinesForSeg(seg.F1)
	f2Lines := splitLinesForSeg(seg.F2)

	if seg.F1 == seg.F2 {
		return nil
	}

	// Pre-index B2 lines for O(1) lookup when classifying.
	b2Set := make(map[string]bool)
	for _, l := range splitLinesForSeg(seg.B2) {
		b2Set[l] = true
	}

	ops := buildDiffOps(f1Lines, f2Lines, PatienceMatches(f1Lines, f2Lines))
	out := make([]DDiffLine, 0, len(ops))
	for _, op := range ops {
		switch op.kind {
		case ' ':
			out = append(out, DDiffLine{Kind: DDiffKept, Text: op.text})
		case '-':
			if b2Set[op.text] {
				out = append(out, DDiffLine{Kind: DDiffAbsorbed, Text: op.text})
			} else {
				out = append(out, DDiffLine{Kind: DDiffDropped, Text: op.text})
			}
		case '+':
			if b2Set[op.text] {
				out = append(out, DDiffLine{Kind: DDiffPropagated, Text: op.text})
			} else {
				out = append(out, DDiffLine{Kind: DDiffAdded, Text: op.text})
			}
		}
	}
	return out
}

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
