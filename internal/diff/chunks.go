package diff

import (
	"regexp"
	"strings"
)

// Chunker detects semantic boundaries in source code and groups hunks
// into reviewable units (functions, types, classes, etc.).
type Chunker interface {
	// Name returns the human-readable name (e.g. "go", "typescript").
	Name() string
	// Chunk splits a file into semantic review units. fileContent is the
	// full file text (for context), hunks are the diff hunks to group.
	Chunk(fileContent string, hunks []Hunk) []Chunk
}

// Chunk is a semantic review unit.
type Chunk struct {
	Signature  string // e.g. "func (h *Handler) CreateThing(w, r)"
	StartLine  int    // 1-based, new file
	EndLine    int    // inclusive
	Complexity int    // cyclomatic complexity estimate
	HunkIdxs   []int  // indices into the hunks slice contained in this chunk
}

// registry maps language names to chunkers.
var registry = map[string]Chunker{}

// RegisterChunker adds a chunker to the global registry. Safe to call at
// init time. Later registrations overwrite earlier ones for the same name.
func RegisterChunker(c Chunker) {
	registry[c.Name()] = c
}

// GetChunker looks up a chunker by language name. Returns nil if not found.
func GetChunker(lang string) Chunker {
	return registry[lang]
}

// RegisteredChunkers returns the sorted list of registered language names.
func RegisteredChunkers() []string {
	names := make([]string, 0, len(registry))
	for k := range registry {
		names = append(names, k)
	}
	return names
}

// --- Default chunkers ---

// boundary marks a semantic boundary (function, type, class, etc.) in a file.
type boundary struct {
	lineNum   int // 1-based
	signature string
}

func init() {
	RegisterChunker(&GoChunker{})
	RegisterChunker(&TypeScriptChunker{})
	RegisterChunker(&PythonChunker{})
}

// --- Go chunker ---

type GoChunker struct{}

func (c *GoChunker) Name() string { return "go" }

var goSignatureRE = regexp.MustCompile(`^(func|type|var|const)\s+`)

func (c *GoChunker) Chunk(fileContent string, hunks []Hunk) []Chunk {
	lines := strings.Split(fileContent, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	var boundaries []boundary
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if goSignatureRE.MatchString(trimmed) {
			boundaries = append(boundaries, boundary{
				lineNum:   i + 1,
				signature: trimmed,
			})
		}
	}

	if len(boundaries) == 0 {
		return nil
	}

	return buildChunksFromBoundaries(boundaries, lines, hunks)
}

// --- TypeScript/JavaScript chunker ---

type TypeScriptChunker struct{}

func (c *TypeScriptChunker) Name() string { return "typescript" }

var tsSignatureRE = regexp.MustCompile(
	`^(export\s+)?(default\s+)?(async\s+)?(function|class|const\s+\w+\s*[=:]\s*(async\s+)?\(|interface|type\s+\w+\s*=)`,
)

func (c *TypeScriptChunker) Chunk(fileContent string, hunks []Hunk) []Chunk {
	lines := strings.Split(fileContent, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	var boundaries []boundary
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if tsSignatureRE.MatchString(trimmed) {
			boundaries = append(boundaries, boundary{
				lineNum:   i + 1,
				signature: trimmed,
			})
		}
	}

	if len(boundaries) == 0 {
		return nil
	}

	return buildChunksFromBoundaries(boundaries, lines, hunks)
}

// --- Python chunker ---

type PythonChunker struct{}

func (c *PythonChunker) Name() string { return "python" }

var pySignatureRE = regexp.MustCompile(`^(async\s+)?(def|class)\s+`)

func (c *PythonChunker) Chunk(fileContent string, hunks []Hunk) []Chunk {
	lines := strings.Split(fileContent, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	var boundaries []boundary
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if pySignatureRE.MatchString(trimmed) {
			boundaries = append(boundaries, boundary{
				lineNum:   i + 1,
				signature: trimmed,
			})
		}
	}

	if len(boundaries) == 0 {
		return nil
	}

	return buildChunksFromBoundaries(boundaries, lines, hunks)
}

// --- Shared chunk building ---

func buildChunksFromBoundaries(boundaries []boundary, fileLines []string, hunks []Hunk) []Chunk {
	var chunks []Chunk

	for bi, b := range boundaries {
		startLine := b.lineNum
		var endLine int
		if bi+1 < len(boundaries) {
			endLine = boundaries[bi+1].lineNum - 1
		} else {
			endLine = len(fileLines)
		}

		// Compute complexity from the chunk's source lines.
		complexity := estimateComplexity(fileLines, startLine-1, endLine-1)

		// Find which hunks overlap with this chunk's line range.
		var hunkIdxs []int
		for hi, h := range hunks {
			r := parseHunkRange(h.Header)
			if r.newStart == 0 {
				continue
			}
			hunkEnd := r.newStart + r.newCount - 1
			// Overlap check: hunk range intersects chunk range.
			if hunkEnd >= startLine && r.newStart <= endLine {
				hunkIdxs = append(hunkIdxs, hi)
			}
		}

		// Skip chunks with no hunks — nothing to review.
		if len(hunkIdxs) == 0 {
			continue
		}

		chunks = append(chunks, Chunk{
			Signature:  b.signature,
			StartLine:  startLine,
			EndLine:    endLine,
			Complexity: complexity,
			HunkIdxs:   hunkIdxs,
		})
	}

	return chunks
}

// estimateComplexity counts branching constructs in the given line range.
// This is a rough cyclomatic complexity estimate.
func estimateComplexity(fileLines []string, startIdx, endIdx int) int {
	if startIdx < 0 {
		startIdx = 0
	}
	if endIdx >= len(fileLines) {
		endIdx = len(fileLines) - 1
	}
	if endIdx < startIdx {
		return 0
	}

	complexity := 1 // base complexity
	for i := startIdx; i <= endIdx; i++ {
		line := strings.TrimSpace(fileLines[i])
		// Skip comments and blank lines.
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "/*") {
			continue
		}
		// Count branching keywords.
		if strings.HasPrefix(line, "if ") || strings.HasPrefix(line, "if\t") || strings.HasPrefix(line, "case ") || strings.HasPrefix(line, "case\t") {
			complexity++
		}
		if strings.HasPrefix(line, "for ") || strings.HasPrefix(line, "for\t") || strings.HasPrefix(line, "while ") || strings.HasPrefix(line, "while\t") {
			complexity++
		}
		if strings.HasPrefix(line, "switch ") || strings.HasPrefix(line, "switch\t") || strings.HasPrefix(line, "match ") || strings.HasPrefix(line, "match\t") {
			complexity++
		}
		// Count logical operators.
		if strings.Contains(line, "&&") || strings.Contains(line, "||") {
			complexity++
		}
		// Ternary operator.
		if strings.Contains(line, "?") && !strings.HasPrefix(line, "//") && !strings.HasPrefix(line, "*") {
			complexity++
		}
	}
	return complexity
}

// hunkRange parses @@ -a,b +c,d @@ from a hunk header.
func parseHunkRange(header string) struct {
	newStart int
	newCount int
} {
	m := hunkHeaderRE.FindStringSubmatch(header)
	if m == nil {
		return struct {
			newStart int
			newCount int
		}{}
	}
	start := 0
	count := 1
	for _, tok := range strings.Fields(m[0]) {
		if strings.HasPrefix(tok, "+") {
			parts := strings.Split(tok[1:], ",")
			if len(parts) > 0 {
				start = atoi(parts[0])
			}
			if len(parts) > 1 {
				count = atoi(parts[1])
			}
			break
		}
	}
	return struct {
		newStart int
		newCount int
	}{newStart: start, newCount: count}
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}
