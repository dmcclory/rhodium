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

// --- FeatureDDiff tests ---

func TestFeatureDDiffIdentical(t *testing.T) {
	seg := Segment{F1: "same", F2: "same"}
	if got := FeatureDDiff(seg); got != nil {
		t.Errorf("identical: got %d lines, want nil", len(got))
	}
}

func TestFeatureDDiffKept(t *testing.T) {
	seg := Segment{F1: "a\nold\nb", F2: "a\nnew\nb"}
	lines := FeatureDDiff(seg)
	// Should have: kept "a", dropped/added for the change, kept "b"
	keptCount := 0
	for _, l := range lines {
		if l.Kind == DDiffKept {
			keptCount++
		}
	}
	if keptCount != 2 {
		t.Errorf("got %d kept lines, want 2: %+v", keptCount, lines)
	}
}

func TestFeatureDDiffDropped(t *testing.T) {
	seg := Segment{
		F1: "keep\ndrop\nkeep2",
		F2: "keep\nkeep2",
		B2: "keep\ndifferent\nkeep2",
	}
	lines := FeatureDDiff(seg)
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3: %+v", len(lines), lines)
	}
	// "drop" was removed and is NOT in B2 → dropped
	found := false
	for _, l := range lines {
		if l.Kind == DDiffDropped && l.Text == "drop" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected DDiffDropped for 'drop': %+v", lines)
	}
}

func TestFeatureDDiffAbsorbed(t *testing.T) {
	seg := Segment{
		F1: "keep\nabsorbed\nkeep2",
		F2: "keep\nkeep2",
		B2: "keep\nabsorbed\nkeep2", // B2 has it → absorbed
	}
	lines := FeatureDDiff(seg)
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3: %+v", len(lines), lines)
	}
	found := false
	for _, l := range lines {
		if l.Kind == DDiffAbsorbed && l.Text == "absorbed" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected DDiffAbsorbed for 'absorbed': %+v", lines)
	}
}

func TestFeatureDDiffAdded(t *testing.T) {
	seg := Segment{
		F1: "old",
		F2: "old\nnew",
		B2: "base\nold", // B2 does NOT have "new"
	}
	lines := FeatureDDiff(seg)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %+v", len(lines), lines)
	}
	found := false
	for _, l := range lines {
		if l.Kind == DDiffAdded && l.Text == "new" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected DDiffAdded for 'new': %+v", lines)
	}
}

func TestFeatureDDiffPropagated(t *testing.T) {
	seg := Segment{
		F1: "old",
		F2: "old\npropagated",
		B2: "base\nold\npropagated", // B2 has "propagated"
	}
	lines := FeatureDDiff(seg)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %+v", len(lines), lines)
	}
	found := false
	for _, l := range lines {
		if l.Kind == DDiffPropagated && l.Text == "propagated" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected DDiffPropagated for 'propagated': %+v", lines)
	}
}
