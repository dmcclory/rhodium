package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

func runCLI(args []string) error {
	switch args[0] {
	case "notes":
		return cmdNotes(args[1:])
	case "todo":
		return cmdTodo(args[1:])
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		printUsage()
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

// splitFlags partitions args into flags (anything starting with -) and positional.
// This lets users pass flags before OR after positional args, which Go's flag
// package doesn't do by default.
func splitFlags(args []string) (flags, positional []string) {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
		} else {
			positional = append(positional, a)
		}
	}
	return
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `rhodium — code review TUI (run with no args) and CLI

Usage:
  rhodium                        launch the TUI
  rhodium notes <owner/repo#N>   print notes for a PR
  rhodium todo                   global dashboard (catch-up, unseen, notes)

Flags:
  --json    emit JSON
  --sync    (todo only) refresh the PR cache from GitHub before printing`)
}

var prRefRE = regexp.MustCompile(`^([^/]+/[^/#]+)[#/](\d+)$`)

func parsePRRef(s string) (repo string, number int, err error) {
	m := prRefRE.FindStringSubmatch(s)
	if m == nil {
		return "", 0, fmt.Errorf("bad PR ref %q — expected owner/repo#123 or owner/repo/123", s)
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return "", 0, err
	}
	return m[1], n, nil
}

// cmdNotes prints notes for a single PR.
func cmdNotes(args []string) error {
	flags, pos := splitFlags(args)
	fs := flag.NewFlagSet("notes", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(flags); err != nil {
		return err
	}
	if len(pos) != 1 {
		return fmt.Errorf("usage: rhodium notes <owner/repo#N>")
	}
	repo, num, err := parsePRRef(pos[0])
	if err != nil {
		return err
	}
	brain, err := LoadBrain()
	if err != nil {
		return err
	}
	defer brain.Close()

	notes := brain.NotesForPR(repo, num)
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(notes)
	}

	if len(notes) == 0 {
		fmt.Printf("%s — no notes\n", prKey(repo, num))
		return nil
	}
	fmt.Printf("%s — %d %s\n\n", prKey(repo, num), len(notes), pluralize("note", len(notes)))
	var curPath string
	for _, n := range notes {
		if n.Path != curPath {
			if curPath != "" {
				fmt.Println()
			}
			fmt.Println(n.Path)
			curPath = n.Path
		}
		fmt.Printf("  line %d  (%s)\n", n.LineNo, n.CreatedAt)
		for _, bl := range strings.Split(strings.TrimRight(n.Body, "\n"), "\n") {
			fmt.Printf("    %s\n", bl)
		}
	}
	return nil
}

// prTodoItem is one PR's row in the todo dashboard.
type prTodoItem struct {
	Key     string   `json:"key"`
	Repo    string   `json:"repo"`
	Number  int      `json:"number"`
	Title   string   `json:"title"`
	Author  string   `json:"author"`
	Tags    []string `json:"tags"`
	Notes   int      `json:"notes,omitempty"`
	CatchUp *struct {
		Done  int `json:"done"`
		Total int `json:"total"`
	} `json:"catch_up,omitempty"`
}

type todoOutput struct {
	PRs []prTodoItem `json:"prs"`
}

// cmdTodo prints a global dashboard of PRs with outstanding review work.
func cmdTodo(args []string) error {
	flags, _ := splitFlags(args)
	fs := flag.NewFlagSet("todo", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit JSON")
	sync := fs.Bool("sync", false, "refresh PR cache from GitHub first")
	if err := fs.Parse(flags); err != nil {
		return err
	}

	brain, err := LoadBrain()
	if err != nil {
		return err
	}
	defer brain.Close()

	if *sync {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		var all []PR
		for _, repo := range cfg.Repos {
			prs, err := listPRs(repo)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warn: %s: %v\n", repo, err)
				continue
			}
			all = append(all, prs...)
		}
		if err := brain.SetPRCache(all); err != nil {
			return fmt.Errorf("write cache: %w", err)
		}
	}

	cached := brain.CachedPRs()
	byKey := map[string]PR{}
	for _, p := range cached {
		byKey[prKey(p.Repo, p.Number)] = p
	}

	catchUps := map[string]*CatchUpSession{}
	sessions := brain.AllActiveCatchUps()
	for i := range sessions {
		catchUps[sessions[i].PRKey] = &sessions[i]
	}

	// Union of all pr_keys with outstanding state — cached PRs plus anything
	// that has notes or an active catch-up (so closed / out-of-window PRs
	// with unresolved notes still surface).
	keys := map[string]bool{}
	for k := range byKey {
		keys[k] = true
	}
	for k := range catchUps {
		keys[k] = true
	}
	for _, k := range brain.PRKeysWithNotes() {
		keys[k] = true
	}

	var items []prTodoItem
	for key := range keys {
		repo, num, err := parsePRRef(key)
		if err != nil {
			continue
		}
		notes := brain.NoteCountForPR(repo, num)
		cu := catchUps[key]
		_, inCache := byKey[key]
		reviewed := len(brain.AllFileReviewedStates(repo, num)) > 0 || brain.HasAnyMarks(repo, num)

		var tags []string
		if cu != nil {
			tags = append(tags, "catch-up")
		}
		if inCache && !reviewed && cu == nil {
			tags = append(tags, "unseen")
		}
		if notes > 0 {
			tags = append(tags, "notes")
		}
		if len(tags) == 0 {
			continue
		}
		p := byKey[key]
		item := prTodoItem{
			Key: key, Repo: repo, Number: num,
			Title: p.Title, Author: p.Author, Tags: tags, Notes: notes,
		}
		if cu != nil {
			item.CatchUp = &struct {
				Done  int `json:"done"`
				Total int `json:"total"`
			}{Done: cu.FilesDone, Total: cu.FilesTotal}
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(todoOutput{PRs: items})
	}

	if len(items) == 0 {
		fmt.Println("todo: nothing pending. (run with --sync to refresh the PR cache)")
		return nil
	}

	fmt.Printf("%d %s need attention\n\n", len(items), pluralize("PR", len(items)))
	for _, it := range items {
		var suffix []string
		if it.CatchUp != nil {
			suffix = append(suffix, fmt.Sprintf("catch-up %d/%d", it.CatchUp.Done, it.CatchUp.Total))
		}
		if contains(it.Tags, "unseen") {
			suffix = append(suffix, "unseen")
		}
		if it.Notes > 0 {
			suffix = append(suffix, fmt.Sprintf("%d %s", it.Notes, pluralize("note", it.Notes)))
		}
		mid := truncate(it.Title, 40)
		if it.Author != "" {
			mid = fmt.Sprintf("%-40s  by %s", mid, it.Author)
		}
		fmt.Printf("  %-28s  %s  [%s]\n", it.Key, mid, strings.Join(suffix, ", "))
	}
	if !*sync {
		fmt.Println("\n(reading cache — use --sync to refresh from GitHub)")
	}
	return nil
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func pluralize(word string, n int) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
