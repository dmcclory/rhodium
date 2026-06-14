package diff

import (
	"strings"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
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
	RegisterChunker(&TSChunker{}) // "treesitter"
	// Register under legacy names for extToLang compatibility.
	RegisterChunker(&tsNamedChunker{named: "go", types: tsBoundaryTypes["go"]})
	RegisterChunker(&tsNamedChunker{named: "typescript", types: tsBoundaryTypes["typescript"]})
	RegisterChunker(&tsNamedChunker{named: "javascript", types: tsBoundaryTypes["javascript"]})
	RegisterChunker(&tsNamedChunker{named: "python", types: tsBoundaryTypes["python"]})
	RegisterChunker(&tsNamedChunker{named: "rust", types: tsBoundaryTypes["rust"]})
	RegisterChunker(&tsNamedChunker{named: "ruby", types: tsBoundaryTypes["ruby"]})
}

// tsNamedChunker delegates to tsChunkForLanguage directly — no trial-and-error.
type tsNamedChunker struct {
	named string
	types []string
}

func (c *tsNamedChunker) Name() string { return c.named }

func (c *tsNamedChunker) Chunk(fileContent string, hunks []Hunk) []Chunk {
	entry := grammars.DetectLanguageByName(c.named)
	if entry == nil {
		return nil
	}
	return tsChunkForLanguage(fileContent, hunks, entry.Language(), c.types)
}

// --- Tree-sitter chunker (replaces per-language regex chunkers) ---

// TSChunker uses tree-sitter ASTs to find top-level declarations as
// chunk boundaries. Works for any language in the gotreesitter grammar
// registry (206 languages). The boundaryTypes map lists AST node types
// that represent semantic boundaries per language.

type TSChunker struct{}

func (c *TSChunker) Name() string { return "treesitter" }

var tsBoundaryTypes = map[string][]string{
	"go":           {"function_declaration", "method_declaration", "type_declaration", "var_declaration", "const_declaration"},
	"typescript":   {"function_declaration", "class_declaration", "interface_declaration", "type_alias_declaration", "lexical_declaration", "variable_declaration", "enum_declaration", "generator_function_declaration"},
	"javascript":   {"function_declaration", "class_declaration", "lexical_declaration", "variable_declaration", "generator_function_declaration"},
	"tsx":          {"function_declaration", "class_declaration", "interface_declaration", "type_alias_declaration", "lexical_declaration", "variable_declaration", "enum_declaration"},
	"python":       {"function_definition", "class_definition"},
	"rust":         {"function_item", "struct_item", "enum_item", "impl_item", "trait_item", "type_item", "const_item", "static_item"},
	"ruby":         {"method", "class", "module", "singleton_class"},
}

// tsTopLevelRoots lists the AST root type names for different languages.
var tsTopLevelRoots = map[string]bool{
	"source_file": true, // Go, C, Rust, etc.
	"module":      true, // Python
	"program":     true, // TypeScript, JavaScript
}

func isTSTopLevel(node *gotreesitter.Node, lang *gotreesitter.Language) bool {
	p := node.Parent()
	if p == nil {
		return true
	}
	return tsTopLevelRoots[p.Type(lang)]
}

func isTSWrappedExport(node *gotreesitter.Node, lang *gotreesitter.Language) bool {
	p := node.Parent()
	return p != nil && p.Type(lang) == "export_statement" && isTSTopLevel(p, lang)
}

func (c *TSChunker) Chunk(fileContent string, hunks []Hunk) []Chunk {
	// Fallback: try all languages. Used when chunker is looked up by
	// name "treesitter" directly (rare; normally extToLang maps to a
	// specific language like "go").
	var bestChunks []Chunk
	for langName, types := range tsBoundaryTypes {
		entry := grammars.DetectLanguageByName(langName)
		if entry == nil {
			continue
		}
		lang := entry.Language()
		chunks := tsChunkForLanguage(fileContent, hunks, lang, types)
		if len(chunks) > len(bestChunks) {
			bestChunks = chunks
		}
	}
	return bestChunks
}

func tsChunkForLanguage(fileContent string, hunks []Hunk, lang *gotreesitter.Language, boundaryTypes []string) []Chunk {
	parser := gotreesitter.NewParser(lang)
	tree, err := parser.Parse([]byte(fileContent))
	if err != nil {
		return nil
	}

	lines := strings.Split(fileContent, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	type tsBoundary struct {
		lineNum   int // 1-based
		signature string
	}
	var boundaries []tsBoundary
	seen := make(map[int]bool)

	var walk func(n *gotreesitter.Node)
	walk = func(n *gotreesitter.Node) {
		t := n.Type(lang)
		for _, bt := range boundaryTypes {
			if t == bt {
				row := int(n.StartPoint().Row) + 1 // 1-based
				if !seen[row] && (isTSTopLevel(n, lang) || isTSWrappedExport(n, lang)) {
					seen[row] = true
					sig := ""
					if row-1 < len(lines) {
						sig = strings.TrimSpace(lines[row-1])
					}
					boundaries = append(boundaries, tsBoundary{lineNum: row, signature: sig})
				}
				break
			}
		}
		for i := 0; i < n.ChildCount(); i++ {
			walk(n.Child(i))
		}
	}
	walk(tree.RootNode())

	if len(boundaries) == 0 {
		return nil
	}

	// Convert tsBoundary to boundary for shared builder.
	boundaries2 := make([]boundary, len(boundaries))
	for i, b := range boundaries {
		boundaries2[i] = boundary{lineNum: b.lineNum, signature: b.signature}
	}
	return buildChunksFromBoundaries(boundaries2, lines, hunks)
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

		complexity := estimateComplexity(fileLines, startLine-1, endLine-1)

		// Find hunks that overlap this chunk's line range. A hunk may
		// appear in multiple chunks; renderChunks filters body lines to
		// each chunk's range so there's no visual duplication.
		var hunkIdxs []int
		for hi, h := range hunks {
			r := parseHunkRange(h.Header)
			if r.newStart == 0 {
				continue
			}
			hunkEnd := r.newStart + r.newCount - 1
			if hunkEnd >= startLine && r.newStart <= endLine {
				hunkIdxs = append(hunkIdxs, hi)
			}
		}

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
