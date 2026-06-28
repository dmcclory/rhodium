package rhodium

import "testing"

func TestDefaultPRViewResolved(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", "files"},
		{"files", "files"},
		{"commits", "commits"},
		{"bogus", "files"},
	}
	for _, tt := range tests {
		c := &Config{DefaultPRView: tt.in}
		if got := c.DefaultPRViewResolved(); got != tt.want {
			t.Errorf("DefaultPRViewResolved(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
