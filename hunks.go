package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
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
	addedStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	deletedStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	lineNumStyle     = lipgloss.NewStyle().Faint(true)
)

type hunkRange struct {
	newStart int
	newCount int
}

var hunkRangeRE = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

func parseHunkRange(header string) hunkRange {
	m := hunkRangeRE.FindStringSubmatch(header)
	if m == nil {
		return hunkRange{}
	}
	start, _ := strconv.Atoi(m[1])
	count := 1
	if m[2] != "" {
		count, _ = strconv.Atoi(m[2])
	}
	return hunkRange{newStart: start, newCount: count}
}

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
			b.WriteString(colorDiffLine(line) + "\n")
			lineNum++
		}
	}
	return b.String(), hunkLines
}

func colorDiffLine(line string) string {
	if len(line) == 0 {
		return line
	}
	switch line[0] {
	case '+':
		return addedStyle.Render(line)
	case '-':
		return deletedStyle.Render(line)
	default:
		return line
	}
}

// renderFullFile produces a full-file view with diff lines colored inline.
// Unchanged lines show with line numbers; additions are green, deletions red.
// Hunk headers with mark indicators are shown at each change boundary.
func renderFullFile(fileContent string, hunks []Hunk, marks map[string]bool, focusedIdx int) (string, []int) {
	fileLines := strings.Split(fileContent, "\n")
	if len(fileLines) > 0 && fileLines[len(fileLines)-1] == "" {
		fileLines = fileLines[:len(fileLines)-1]
	}

	type parsedHunk struct {
		Hunk
		r hunkRange
	}
	parsed := make([]parsedHunk, len(hunks))
	for i, h := range hunks {
		parsed[i] = parsedHunk{Hunk: h, r: parseHunkRange(h.Header)}
	}

	var b strings.Builder
	hunkLineOffsets := make([]int, len(hunks))
	outputLine := 0
	newFileLine := 1 // 1-indexed line in the new file
	gutterW := len(fmt.Sprintf("%d", len(fileLines)+100))

	writeLine := func(num int, text string) {
		gutter := lineNumStyle.Render(fmt.Sprintf("%*d", gutterW, num))
		b.WriteString(gutter + "  " + text + "\n")
		outputLine++
	}
	writeUnnum := func(text string) {
		pad := strings.Repeat(" ", gutterW)
		b.WriteString(pad + "  " + text + "\n")
		outputLine++
	}

	for hi, ph := range parsed {
		// Plain lines before this hunk.
		for newFileLine < ph.r.newStart && newFileLine-1 < len(fileLines) {
			writeLine(newFileLine, fileLines[newFileLine-1])
			newFileLine++
		}

		// Hunk header.
		mark := "[ ]"
		if marks[ph.Hash] {
			mark = markedStyle.Render("[✓]")
		}
		headerLine := mark + " " + ph.Header
		if hi == focusedIdx {
			headerLine = focusedHunkStyle.Render(headerLine)
		}
		hunkLineOffsets[hi] = outputLine
		writeUnnum(headerLine)

		// Replay hunk body.
		for _, line := range ph.BodyLines {
			if len(line) == 0 {
				writeLine(newFileLine, "")
				newFileLine++
				continue
			}
			switch line[0] {
			case '+':
				writeLine(newFileLine, addedStyle.Render(line[1:]))
				newFileLine++
			case '-':
				writeUnnum(deletedStyle.Render(line))
			default: // context line (space prefix)
				text := line
				if len(text) > 0 && text[0] == ' ' {
					text = text[1:]
				}
				writeLine(newFileLine, text)
				newFileLine++
			}
		}
	}

	// Remaining lines after last hunk.
	for newFileLine-1 < len(fileLines) {
		writeLine(newFileLine, fileLines[newFileLine-1])
		newFileLine++
	}

	return b.String(), hunkLineOffsets
}
