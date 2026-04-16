package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// PRRef identifies a PR by repo + number, without any metadata.
type PRRef struct {
	Repo   string
	Number int
}

// FileStatus is the reviewer's per-file state: none / some / all hunks marked.
type FileStatus int

const (
	StatusUnseen  FileStatus = iota // no hunks marked
	StatusPartial                   // some but not all current hunks marked
	StatusSeen                      // every current hunk is marked, or the file has no hunks
)

func (s FileStatus) Glyph() string {
	switch s {
	case StatusSeen:
		return "✓"
	case StatusPartial:
		return "◐"
	default:
		return " "
	}
}

// Brain is the reviewer's private knowledge state. Keyed by (repo, pr, path);
// each entry stores a set of hunk content hashes the reviewer has marked.
//
// Marks are per-hunk, content-hashed, so they survive rebases and amends as
// long as the hunk's actual +/- lines don't change.
type Brain struct {
	mu   sync.Mutex
	path string
	data brainData
}

type brainData struct {
	Seen map[string]map[string]seenEntry `json:"seen"` // "owner/repo#123" → path → entry
}

type seenEntry struct {
	Hunks []string `json:"hunks,omitempty"` // sorted set of hunk content hashes
}

func prKey(repo string, number int) string {
	return fmt.Sprintf("%s#%d", repo, number)
}

func brainPath() (string, error) {
	if p := os.Getenv("RHODIUM_BRAIN"); p != "" {
		return p, nil
	}
	dir := os.Getenv("XDG_DATA_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dir, "rhodium", "brain.json"), nil
}

func LoadBrain() (*Brain, error) {
	path, err := brainPath()
	if err != nil {
		return nil, err
	}
	b := &Brain{path: path, data: brainData{Seen: map[string]map[string]seenEntry{}}}

	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return b, nil
		}
		return nil, err
	}
	if len(raw) == 0 {
		return b, nil
	}
	if err := json.Unmarshal(raw, &b.data); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if b.data.Seen == nil {
		b.data.Seen = map[string]map[string]seenEntry{}
	}
	return b, nil
}

func (b *Brain) save() error {
	if err := os.MkdirAll(filepath.Dir(b.path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(b.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := b.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, b.path)
}

// HasAnyMarks reports whether the reviewer has marked any hunk in the given
// PR. Fast enough to call without having fetched the PR's file list.
func (b *Brain) HasAnyMarks(repo string, pr int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	seen := b.data.Seen[prKey(repo, pr)]
	for _, e := range seen {
		if len(e.Hunks) > 0 {
			return true
		}
	}
	return false
}

// InProgressRefs returns every PR the reviewer has marked any hunk in. Cheap:
// reads the in-memory brain only, no network. Used to render in-progress PRs
// before the full `gh pr list` results arrive.
func (b *Brain) InProgressRefs() []PRRef {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []PRRef
	for key, files := range b.data.Seen {
		has := false
		for _, e := range files {
			if len(e.Hunks) > 0 {
				has = true
				break
			}
		}
		if !has {
			continue
		}
		idx := strings.LastIndex(key, "#")
		if idx < 0 {
			continue
		}
		var num int
		if _, err := fmt.Sscanf(key[idx+1:], "%d", &num); err != nil {
			continue
		}
		out = append(out, PRRef{Repo: key[:idx], Number: num})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Repo != out[j].Repo {
			return out[i].Repo < out[j].Repo
		}
		return out[i].Number < out[j].Number
	})
	return out
}

// HunkMarks returns the set of marked hunk hashes for a file.
func (b *Brain) HunkMarks(repo string, pr int, path string) map[string]bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	seen := b.data.Seen[prKey(repo, pr)]
	if seen == nil {
		return map[string]bool{}
	}
	e := seen[path]
	out := make(map[string]bool, len(e.Hunks))
	for _, h := range e.Hunks {
		out[h] = true
	}
	return out
}

// SetHunkMarks replaces the marked set for a file.
func (b *Brain) SetHunkMarks(repo string, pr int, path string, marks map[string]bool) error {
	b.mu.Lock()
	key := prKey(repo, pr)
	if b.data.Seen[key] == nil {
		b.data.Seen[key] = map[string]seenEntry{}
	}
	hashes := make([]string, 0, len(marks))
	for h, on := range marks {
		if on {
			hashes = append(hashes, h)
		}
	}
	sort.Strings(hashes)
	if len(hashes) == 0 {
		delete(b.data.Seen[key], path)
	} else {
		b.data.Seen[key][path] = seenEntry{Hunks: hashes}
	}
	b.mu.Unlock()
	return b.save()
}

// Status reports the review state of a file by hashing its current hunks and
// comparing to the marked set.
func (b *Brain) Status(repo string, pr int, fc FileChange) FileStatus {
	hunks := parseHunks(fc.Patch)
	if len(hunks) == 0 {
		return StatusSeen
	}
	marks := b.HunkMarks(repo, pr, fc.Path)
	matched := 0
	for _, h := range hunks {
		if marks[h.Hash] {
			matched++
		}
	}
	switch {
	case matched == 0:
		return StatusUnseen
	case matched == len(hunks):
		return StatusSeen
	default:
		return StatusPartial
	}
}

// UnseenCount returns the number of files that aren't fully seen (unseen +
// partial).
func (b *Brain) UnseenCount(repo string, pr int, files []FileChange) int {
	n := 0
	for _, f := range files {
		if b.Status(repo, pr, f) != StatusSeen {
			n++
		}
	}
	return n
}
