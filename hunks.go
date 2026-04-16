package main

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type Hunk struct {
	Header    string   // the @@ -a,b +c,d @@ line, verbatim
	BodyLines []string // lines after the header, up to the next hunk
	Hash      string   // content hash of the +/- lines only
}

var hunkHeaderRE = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+\d+(?:,\d+)? @@`)

// parseHunks splits a unified-diff patch into hunks. File header lines
// above the first `@@` (diff --git, ---, +++) are dropped — only hunks
// themselves are review-markable units.
func parseHunks(patch string) []Hunk {
	if patch == "" {
		return nil
	}
	lines := strings.Split(patch, "\n")
	var hunks []Hunk
	var cur *Hunk
	flush := func() {
		if cur == nil {
			return
		}
		cur.Hash = hashHunkBody(cur.BodyLines)
		hunks = append(hunks, *cur)
		cur = nil
	}
	for _, line := range lines {
		if hunkHeaderRE.MatchString(line) {
			flush()
			cur = &Hunk{Header: line}
			continue
		}
		if cur != nil {
			cur.BodyLines = append(cur.BodyLines, line)
		}
	}
	flush()
	// The last hunk often has a trailing empty string from the final newline.
	// Trim it so hashing is stable across trailing-newline variations.
	for i := range hunks {
		body := hunks[i].BodyLines
		if len(body) > 0 && body[len(body)-1] == "" {
			hunks[i].BodyLines = body[:len(body)-1]
			hunks[i].Hash = hashHunkBody(hunks[i].BodyLines)
		}
	}
	return hunks
}

// hashHunkBody hashes only the +/- lines of a hunk. Context shifts (e.g., an
// unrelated insertion earlier in the file) don't change the hash, so marks
// survive rebases and amends that don't touch this region.
func hashHunkBody(lines []string) string {
	var kept []string
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		switch line[0] {
		case '+', '-':
			kept = append(kept, line)
		}
	}
	sum := sha256.Sum256([]byte(strings.Join(kept, "\n")))
	return hex.EncodeToString(sum[:])[:16]
}

var (
	focusedHunkStyle = lipgloss.NewStyle().Reverse(true).Bold(true)
	markedStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
)

// renderHunks produces the diff body with a "[✓]"/"[ ]" marker prepended to
// each hunk header. The hunk at focusedIdx is rendered with a reverse-video
// header so you can see what `space` / `up` / `down` will act on. Returns
// the rendered body and a parallel slice with each hunk's header line offset
// for SetYOffset-based navigation.
func renderHunks(hunks []Hunk, marks map[string]bool, focusedIdx int) (string, []int) {
	var b strings.Builder
	hunkLines := make([]int, 0, len(hunks))
	lineNum := 0
	for i, h := range hunks {
		mark := "[ ]"
		if marks[h.Hash] {
			mark = markedStyle.Render("[✓]")
		}
		headerLine := mark + " " + h.Header
		if i == focusedIdx {
			headerLine = focusedHunkStyle.Render(headerLine)
		}
		hunkLines = append(hunkLines, lineNum)
		b.WriteString(headerLine + "\n")
		lineNum++
		for _, line := range h.BodyLines {
			b.WriteString(line + "\n")
			lineNum++
		}
	}
	return b.String(), hunkLines
}
