package diff

import "testing"

func TestGoChunker(t *testing.T) {
	c := &GoChunker{}
	if c.Name() != "go" {
		t.Errorf("Name() = %q, want %q", c.Name(), "go")
	}

	fileContent := `package main

import "fmt"

func main() {
	fmt.Println("hello")
}

func add(a, b int) int {
	return a + b
}

type Config struct {
	Name string
	Port int
}

func (c *Config) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("name required")
	}
	if c.Port <= 0 {
		return fmt.Errorf("port must be positive")
	}
	return nil
}

const defaultPort = 8080

var globalConfig = &Config{Port: defaultPort}
`

	hunks := []Hunk{
		{Header: "@@ -0,0 +1,5 @@", BodyLines: []string{"+package main", "+", `+import "fmt"`, "+", "+func main() {"}, Hash: "hash1"},
		{Header: "@@ -0,0 +6,8 @@", BodyLines: []string{"+\tfmt.Println(\"hello\")", "+}", "+", "+func add(a, b int) int {", "+\treturn a + b", "+}", "+", "+type Config struct {"}, Hash: "hash2"},
		{Header: "@@ -0,0 +14,10 @@", BodyLines: []string{"+\tName string", "+\tPort int", "+}", "+", "+func (c *Config) Validate() error {", "+\tif c.Name == \"\" {", "+\t\treturn fmt.Errorf(\"name required\")", "+\t}", "+\tif c.Port <= 0 {", "+\t\treturn fmt.Errorf(\"port must be positive\")"}, Hash: "hash3"},
		{Header: "@@ -0,0 +24,6 @@", BodyLines: []string{"+\treturn nil", "+}", "+", "+const defaultPort = 8080", "+", "+var globalConfig = &Config{Port: defaultPort}"}, Hash: "hash4"},
	}

	chunks := c.Chunk(fileContent, hunks)

	// With one-chunk-per-hunk semantics, hunks are assigned to the chunk
	// where their start line falls. Hunk 1 (line 1) → func main (before
	// first boundary, assigned to first chunk). Hunk 2 (line 6) → func main.
	// Hunk 3 (line 14) → type Config. Hunk 4 (line 24) → Validate.
	// func add, const, and var get no hunks and are skipped. Result: 3 chunks.
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(chunks))
	}

	expected := []struct {
		sig   string
		start int
		end   int
	}{
		{"func main() {", 5, 8},
		{"type Config struct {", 13, 17},
		{"func (c *Config) Validate() error {", 18, 27},
	}

	for i, want := range expected {
		ch := chunks[i]
		if ch.Signature != want.sig {
			t.Errorf("chunk[%d].Signature = %q, want %q", i, ch.Signature, want.sig)
		}
		if ch.StartLine != want.start {
			t.Errorf("chunk[%d].StartLine = %d, want %d", i, ch.StartLine, want.start)
		}
		if ch.EndLine != want.end {
			t.Errorf("chunk[%d].EndLine = %d, want %d", i, ch.EndLine, want.end)
		}
	}
}

func TestGoChunkerNoBoundaries(t *testing.T) {
	c := &GoChunker{}
	fileContent := `package main

import "fmt"

func main() {
	fmt.Println("hello")
}
`
	// All hunks are within the same function — should produce 1 chunk.
	hunks := []Hunk{
		{Header: "@@ -0,0 +1,5 @@", BodyLines: []string{"+package main", "+", `+import "fmt"`}, Hash: "h1"},
	}

	chunks := c.Chunk(fileContent, hunks)

	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1", len(chunks))
	}
	if chunks[0].Signature != "func main() {" {
		t.Errorf("Signature = %q, want %q", chunks[0].Signature, "func main() {")
	}
}

func TestGoChunkerEmpty(t *testing.T) {
	c := &GoChunker{}
	chunks := c.Chunk("", nil)
	if chunks != nil {
		t.Fatalf("got %d chunks, want nil", len(chunks))
	}
}

func TestTypeScriptChunker(t *testing.T) {
	c := &TypeScriptChunker{}
	if c.Name() != "typescript" {
		t.Errorf("Name() = %q, want %q", c.Name(), "typescript")
	}

	fileContent := `export class UserService {
  constructor(private db: Database) {}

  async getUser(id: string): Promise<User> {
    return this.db.find(id);
  }

  async createUser(user: CreateUserInput): Promise<User> {
    if (!user.email) {
      throw new Error("email required");
    }
    return this.db.insert(user);
  }
}

export interface User {
  id: string;
  email: string;
}

export type CreateUserInput = Omit<User, "id">;

export const DEFAULT_PAGE_SIZE = 20;

export function formatUser(user: User): string {
  return user.email;
}
`

	hunks := []Hunk{
		{Header: "@@ -0,0 +1,15 @@", BodyLines: []string{"+export class UserService {", "+  constructor(private db: Database) {}", "+", "+  async getUser(id: string): Promise<User> {", "+    return this.db.find(id);", "+  }", "+", "+  async createUser(user: CreateUserInput): Promise<User> {", "+    if (!user.email) {", "+      throw new Error(\"email required\");", "+    }", "+    return this.db.insert(user);", "+  }", "+}", ""}, Hash: "ts1"},
		{Header: "@@ -0,0 +16,8 @@", BodyLines: []string{"+export interface User {", "+  id: string;", "+  email: string;", "+}", "+", "+export type CreateUserInput = Omit<User, \"id\">;", "+", "+export const DEFAULT_PAGE_SIZE = 20;"}, Hash: "ts2"},
		{Header: "@@ -0,0 +24,4 @@", BodyLines: []string{"+export function formatUser(user: User): string {", "+  return user.email;", "+}", ""}, Hash: "ts3"},
	}

	chunks := c.Chunk(fileContent, hunks)

	// One-chunk-per-hunk: ts1 (line 1) → class UserService, ts2 (line 16)
	// → interface User, ts3 (line 24) → formatUser. Result: 3 chunks.
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(chunks))
	}

	expected := []struct {
		sig string
	}{
		{"export class UserService {"},
		{"export interface User {"},
		{"export type CreateUserInput = Omit<User, \"id\">;"},
	}

	for i, want := range expected {
		if chunks[i].Signature != want.sig {
			t.Errorf("chunk[%d].Signature = %q, want %q", i, chunks[i].Signature, want.sig)
		}
	}
}

func TestPythonChunker(t *testing.T) {
	c := &PythonChunker{}
	if c.Name() != "python" {
		t.Errorf("Name() = %q, want %q", c.Name(), "python")
	}

	fileContent := `class UserService:
    def __init__(self, db):
        self.db = db

    def get_user(self, user_id):
        return self.db.find(user_id)

    def create_user(self, user_data):
        if not user_data.get("email"):
            raise ValueError("email required")
        return self.db.insert(user_data)

def format_user(user):
    return user["email"]
`

	hunks := []Hunk{
		{Header: "@@ -0,0 +1,12 @@", BodyLines: []string{"+class UserService:", "+    def __init__(self, db):", "+        self.db = db", "+", "+    def get_user(self, user_id):", "+        return self.db.find(user_id)", "+", "+    def create_user(self, user_data):", "+        if not user_data.get(\"email\"):", "+            raise ValueError(\"email required\")", "+        return self.db.insert(user_data)"}, Hash: "py1"},
		{Header: "@@ -0,0 +13,3 @@", BodyLines: []string{"+def format_user(user):", "+    return user[\"email\"]", ""}, Hash: "py2"},
	}

	chunks := c.Chunk(fileContent, hunks)

	// One-chunk-per-hunk: py1 (line 1) → class UserService, py2 (line 13)
	// → format_user. Result: 2 chunks.
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}

	expected := []struct {
		sig string
	}{
		{"class UserService:"},
		{"def format_user(user):"},
	}

	for i, want := range expected {
		if chunks[i].Signature != want.sig {
			t.Errorf("chunk[%d].Signature = %q, want %q", i, chunks[i].Signature, want.sig)
		}
	}
}

func TestEstimateComplexity(t *testing.T) {
	tests := []struct {
		name     string
		lines    []string
		wantMin  int // at least this complex
	}{
		{
			name: "simple function",
			lines: []string{
				"func simple() {",
				"\treturn 42",
				"}",
			},
			wantMin: 1,
		},
		{
			name: "function with if",
			lines: []string{
				"func withIf(x int) int {",
				"\tif x > 0 {",
				"\t\treturn x",
				"\t}",
				"\treturn 0",
				"}",
			},
			wantMin: 2,
		},
		{
			name: "complex function",
			lines: []string{
				"func complex(x, y int) int {",
				"\tif x > 0 && y > 0 {",
				"\t\tfor i := 0; i < x; i++ {",
				"\t\t\tswitch i {",
				"\t\t\tcase 1:",
				"\t\t\t\treturn i",
				"\t\t\tcase 2:",
				"\t\t\t\treturn i * 2",
				"\t\t\t}",
				"\t\t}",
				"\t} else if x < 0 || y < 0 {",
				"\t\treturn -1",
				"\t}",
				"\treturn 0",
				"}",
			},
			wantMin: 8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimateComplexity(tt.lines, 0, len(tt.lines)-1)
			if got < tt.wantMin {
				t.Errorf("complexity = %d, want at least %d", got, tt.wantMin)
			}
		})
	}
}

func TestChunkerRegistry(t *testing.T) {
	// Verify default chunkers are registered.
	for _, name := range []string{"go", "typescript", "python"} {
		c := GetChunker(name)
		if c == nil {
			t.Errorf("GetChunker(%q) = nil, want non-nil", name)
		}
		if c.Name() != name {
			t.Errorf("GetChunker(%q).Name() = %q, want %q", name, c.Name(), name)
		}
	}

	// Unknown language returns nil.
	if c := GetChunker("rust"); c != nil {
		t.Errorf("GetChunker(\"rust\") = %v, want nil", c)
	}

	// RegisteredChunkers returns the defaults.
	names := RegisteredChunkers()
	if len(names) < 3 {
		t.Errorf("RegisteredChunkers() has %d entries, want at least 3", len(names))
	}
}

type testCustomChunker struct{}

func (c *testCustomChunker) Name() string { return "custom" }
func (c *testCustomChunker) Chunk(fileContent string, hunks []Hunk) []Chunk {
	return []Chunk{{Signature: "custom", StartLine: 1, EndLine: 1}}
}

func TestCustomChunker(t *testing.T) {
	RegisterChunker(&testCustomChunker{})

	c := GetChunker("custom")
	if c == nil {
		t.Fatal("GetChunker(\"custom\") = nil")
	}
	if c.Name() != "custom" {
		t.Errorf("Name() = %q, want %q", c.Name(), "custom")
	}
}

func TestBuildChunksSkipsChunksWithNoHunks(t *testing.T) {
	// A file with 3 functions but only 2 have hunks.
	fileContent := `func existing() {
	// no changes here
}

func newFunc() {
	fmt.Println("new")
}

func another() {
	// also no changes
}

func modified() {
	fmt.Println("modified")
}
`

	hunks := []Hunk{
		{Header: "@@ -0,0 +5,3 @@", BodyLines: []string{"+func newFunc() {", `+\tfmt.Println("new")`, "+}"}, Hash: "h1"},
		{Header: "@@ -0,0 +13,3 @@", BodyLines: []string{"+func modified() {", `+\tfmt.Println("modified")`, "+}"}, Hash: "h2"},
	}

	c := &GoChunker{}
	chunks := c.Chunk(fileContent, hunks)

	// Should only return 2 chunks — the ones with hunks.
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}
	if chunks[0].Signature != "func newFunc() {" {
		t.Errorf("chunk[0].Signature = %q, want %q", chunks[0].Signature, "func newFunc() {")
	}
	if chunks[1].Signature != "func modified() {" {
		t.Errorf("chunk[1].Signature = %q, want %q", chunks[1].Signature, "func modified() {")
	}
}

func TestParseHunkRangeInChunks(t *testing.T) {
	tests := []struct {
		header   string
		wantStart int
		wantCount int
	}{
		{"@@ -0,0 +1,5 @@", 1, 5},
		{"@@ -10,3 +20,8 @@", 20, 8},
		{"@@ -100,50 +200,1 @@", 200, 1},
		{"invalid header", 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.header, func(t *testing.T) {
			r := parseHunkRange(tt.header)
			if r.newStart != tt.wantStart {
				t.Errorf("newStart = %d, want %d", r.newStart, tt.wantStart)
			}
			if r.newCount != tt.wantCount {
				t.Errorf("newCount = %d, want %d", r.newCount, tt.wantCount)
			}
		})
	}
}
