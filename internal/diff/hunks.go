package diff

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

type Hunk struct {
	Header    string   // the @@ -a,b +c,d @@ line, verbatim
	BodyLines []string // lines after the header, up to the next hunk
	Hash      string   // content hash of the +/- lines only
}

// IsMarkable distinguishes real diff hunks (hashed +/- content the reviewer
// can tick off) from synthetic segment-header hunks (Hash==""), which only
// exist to render a boundary label between the real hunks of a segmented
// slow-path view.
func (h Hunk) IsMarkable() bool { return h.Hash != "" }

var hunkHeaderRE = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+\d+(?:,\d+)? @@`)

// ParseHunks splits a unified-diff patch into hunks. File header lines
// above the first `@@` (diff --git, ---, +++) are dropped — only hunks
// themselves are review-markable units.
func ParseHunks(patch string) []Hunk {
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
		cur.Hash = HashHunkBody(cur.BodyLines)
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
			hunks[i].Hash = HashHunkBody(hunks[i].BodyLines)
		}
	}
	return hunks
}

// HashHunkBody hashes only the +/- lines of a hunk. Context shifts (e.g., an
// unrelated insertion earlier in the file) don't change the hash, so marks
// survive rebases and amends that don't touch this region.
func HashHunkBody(lines []string) string {
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
