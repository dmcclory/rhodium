package cli

import (
	"reflect"
	"testing"
)

func TestSplitFlags_BoolOnly(t *testing.T) {
	// Existing callers only use bool flags; this must keep working.
	f, p := splitFlags([]string{"--json", "owner/repo#42", "--sync"})
	if !reflect.DeepEqual(f, []string{"--json", "--sync"}) {
		t.Errorf("flags: got %v", f)
	}
	if !reflect.DeepEqual(p, []string{"owner/repo#42"}) {
		t.Errorf("positional: got %v", p)
	}
}

func TestSplitFlags_ValueTakingFlags(t *testing.T) {
	// --limit 20 should be grouped into flags, not treated as positional.
	f, p := splitFlags([]string{"--limit", "20", "--pr", "owner/repo#42"}, "pr", "limit")
	if !reflect.DeepEqual(f, []string{"--limit", "20", "--pr", "owner/repo#42"}) {
		t.Errorf("flags: got %v", f)
	}
	if len(p) != 0 {
		t.Errorf("positional: got %v, want empty", p)
	}
}

func TestSplitFlags_ValueAfterPositional(t *testing.T) {
	// Flags can appear after positional args (this is why splitFlags exists).
	f, p := splitFlags([]string{"owner/repo#42", "--limit", "10"}, "limit")
	if !reflect.DeepEqual(f, []string{"--limit", "10"}) {
		t.Errorf("flags: got %v", f)
	}
	if !reflect.DeepEqual(p, []string{"owner/repo#42"}) {
		t.Errorf("positional: got %v", p)
	}
}

func TestSplitFlags_ValueFlagFollowedByAnotherFlag(t *testing.T) {
	// --limit followed by --json (bool): 20 should NOT be consumed if
	// the next arg looks like a flag.
	f, p := splitFlags([]string{"--limit", "--json"}, "limit")
	if !reflect.DeepEqual(f, []string{"--limit", "--json"}) {
		t.Errorf("flags: got %v", f)
	}
	if len(p) != 0 {
		t.Errorf("positional: got %v, want empty", p)
	}
}

func TestSplitFlags_MixedFlagsAndPositional(t *testing.T) {
	f, p := splitFlags([]string{"--json", "owner/repo#42", "--kind", "note.", "--limit", "5"}, "kind", "limit")
	if !reflect.DeepEqual(f, []string{"--json", "--kind", "note.", "--limit", "5"}) {
		t.Errorf("flags: got %v", f)
	}
	if !reflect.DeepEqual(p, []string{"owner/repo#42"}) {
		t.Errorf("positional: got %v", p)
	}
}

func TestSplitFlags_SingleDash(t *testing.T) {
	// Single-dash flags should work too.
	f, p := splitFlags([]string{"-limit", "10", "foo"}, "limit")
	if !reflect.DeepEqual(f, []string{"-limit", "10"}) {
		t.Errorf("flags: got %v", f)
	}
	if !reflect.DeepEqual(p, []string{"foo"}) {
		t.Errorf("positional: got %v", p)
	}
}

func TestSplitFlags_NoValueFlags(t *testing.T) {
	// When no valueFlags are declared, the old behavior is preserved.
	f, p := splitFlags([]string{"--json", "--all", "owner/repo#42"})
	if !reflect.DeepEqual(f, []string{"--json", "--all"}) {
		t.Errorf("flags: got %v", f)
	}
	if !reflect.DeepEqual(p, []string{"owner/repo#42"}) {
		t.Errorf("positional: got %v", p)
	}
}

func TestSplitFlags_EmptyInput(t *testing.T) {
	f, p := splitFlags([]string{}, "limit")
	if len(f) != 0 || len(p) != 0 {
		t.Errorf("empty: got flags=%v, positional=%v", f, p)
	}
}

func TestParsePRRef(t *testing.T) {
	tests := []struct {
		input       string
		wantRepo    string
		wantNumber  int
		wantErr     bool
	}{
		{"owner/repo#42", "owner/repo", 42, false},
		{"owner/repo/42", "owner/repo", 42, false},
		{"acme/web#1", "acme/web", 1, false},
		{"cli/cli#11569", "cli/cli", 11569, false},
		{"", "", 0, true},
		{"just-a-string", "", 0, true},
		{"owner/repo", "", 0, true},
		{"owner/repo#abc", "", 0, true},
	}
	for _, tt := range tests {
		repo, num, err := parsePRRef(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Errorf("parsePRRef(%q): expected error, got nil", tt.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("parsePRRef(%q): unexpected error: %v", tt.input, err)
			continue
		}
		if repo != tt.wantRepo || num != tt.wantNumber {
			t.Errorf("parsePRRef(%q): got (%q, %d), want (%q, %d)",
				tt.input, repo, num, tt.wantRepo, tt.wantNumber)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input string
		max   int
		want  string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello world", 7, "hello …"},
		{"hello world", 8, "hello w…"},
		{"a", 1, "a"},
		{"", 5, ""},
	}
	for _, tt := range tests {
		got := truncate(tt.input, tt.max)
		if got != tt.want {
			t.Errorf("truncate(%q, %d): got %q, want %q", tt.input, tt.max, got, tt.want)
		}
	}
}

func TestPluralize(t *testing.T) {
	if pluralize("note", 0) != "notes" {
		t.Error("pluralize(0): want 'notes'")
	}
	if pluralize("note", 1) != "note" {
		t.Error("pluralize(1): want 'note'")
	}
	if pluralize("note", 2) != "notes" {
		t.Error("pluralize(2): want 'notes'")
	}
	if pluralize("file", 1) != "file" {
		t.Error("pluralize(1): want 'file'")
	}
}
