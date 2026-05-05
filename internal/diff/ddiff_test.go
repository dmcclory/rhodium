package diff

import "testing"

func TestStorySummaryEmpty(t *testing.T) {
	seg := Segment{B1: "", F1: "", B2: "", F2: ""}
	if got := StorySummary(seg); got != "" {
		t.Errorf("empty segment: got %q, want empty", got)
	}
}

func TestStorySummaryPureAddition(t *testing.T) {
	// Nothing in f1 — all new lines in f2.
	seg := Segment{F1: "", F2: "new1\nnew2\nnew3"}
	got := StorySummary(seg)
	if got != "story: +3 new lines → f2 has 3" {
		t.Errorf("got %q", got)
	}
}

func TestStorySummaryPureDeletion(t *testing.T) {
	// f2 is empty — everything dropped.
	seg := Segment{F1: "old1\nold2", F2: ""}
	got := StorySummary(seg)
	if got != "story: f1 had 2 lines; rebase dropped all" {
		t.Errorf("got %q", got)
	}
}

func TestStorySummaryIdentical(t *testing.T) {
	// f1 == f2 — no rebase delta.
	seg := Segment{F1: "same\nlines", F2: "same\nlines"}
	if got := StorySummary(seg); got != "" {
		t.Errorf("identical f1/f2: got %q, want empty", got)
	}
}

func TestStorySummarySimpleReplacement(t *testing.T) {
	// f1 has "old", f2 has "new" — one dropped, one added.
	seg := Segment{F1: "old", F2: "new"}
	got := StorySummary(seg)
	if got != "story: f1 had 1 lines; rebase dropped 1; added 1 → f2 has 1" {
		t.Errorf("got %q", got)
	}
}

func TestStorySummaryComplex(t *testing.T) {
	// f1: 3 lines, f2: 4 lines — dropped 1, added 2.
	seg := Segment{
		F1: "keep\ndrop\nkeep2",
		F2: "keep\nadd1\nkeep2\nadd2",
	}
	got := StorySummary(seg)
	if got != "story: f1 had 3 lines; rebase dropped 1; added 2 → f2 has 4" {
		t.Errorf("got %q", got)
	}
}
