package brain

import (
	"rhodium/internal/diff"
	"rhodium/internal/gh"
)

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

// IsScrutinized returns whether a PR is marked for full scrutiny.
func (b *Brain) IsScrutinized(repo string, pr int) bool {
	key := PRKey(repo, pr)
	var exists bool
	b.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pr_scrutiny WHERE pr_key = ?)`, key).Scan(&exists)
	return exists
}

// SetScrutiny marks or unmarks a PR for scrutiny.
func (b *Brain) SetScrutiny(repo string, pr int, on bool) error {
	key := PRKey(repo, pr)
	tx, err := b.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if on {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO pr_scrutiny (pr_key) VALUES (?)`, key); err != nil {
			return err
		}
	} else {
		if _, err := tx.Exec(`DELETE FROM pr_scrutiny WHERE pr_key = ?`, key); err != nil {
			return err
		}
	}
	if err := logEvent(tx, "scrutiny.set", key, "", map[string]any{"on": on}); err != nil {
		return err
	}
	return tx.Commit()
}

func (b *Brain) HasAnyMarks(repo string, pr int) bool {
	key := PRKey(repo, pr)
	var exists bool
	b.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM hunk_marks WHERE pr_key = ?)`, key).Scan(&exists)
	return exists
}

func (b *Brain) HunkMarks(repo string, pr int, path string) map[string]bool {
	key := PRKey(repo, pr)
	rows, err := b.db.Query(`SELECT hunk_hash FROM hunk_marks WHERE pr_key = ? AND path = ?`, key, path)
	if err != nil {
		return map[string]bool{}
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var h string
		if rows.Scan(&h) == nil {
			out[h] = true
		}
	}
	return out
}

func (b *Brain) SetHunkMarks(repo string, pr int, path string, marks map[string]bool) error {
	key := PRKey(repo, pr)
	tx, err := b.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Snapshot prior marks before the bulk replace so we can emit one event
	// per actual toggle (rather than one coarse "marks.replace"). Per-hunk
	// events make future per-hunk undo trivial.
	prior := map[string]bool{}
	rows, err := tx.Query(`SELECT hunk_hash FROM hunk_marks WHERE pr_key = ? AND path = ?`, key, path)
	if err != nil {
		return err
	}
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			rows.Close()
			return err
		}
		prior[h] = true
	}
	if err := rows.Close(); err != nil {
		return err
	}

	if _, err := tx.Exec(`DELETE FROM hunk_marks WHERE pr_key = ? AND path = ?`, key, path); err != nil {
		return err
	}
	for h, on := range marks {
		if on {
			if _, err := tx.Exec(`INSERT INTO hunk_marks (pr_key, path, hunk_hash) VALUES (?, ?, ?)`, key, path, h); err != nil {
				return err
			}
		}
	}
	for h, on := range marks {
		if on && !prior[h] {
			if err := logEvent(tx, "mark.set", key, path, map[string]string{"hunk_hash": h}); err != nil {
				return err
			}
		}
	}
	for h := range prior {
		if !marks[h] {
			if err := logEvent(tx, "mark.clear", key, path, map[string]string{"hunk_hash": h}); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (b *Brain) Status(repo string, pr int, fc gh.FileChange) FileStatus {
	hunks := diff.ParseHunks(fc.Patch)
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

// FileReviewState holds the base and head SHAs at which a file was last reviewed.
type FileReviewState struct {
	HeadSHA string
	BaseSHA string
}

// SetFileReviewed records the PR head and base SHAs at which a file was last
// reviewed. Called alongside mark saves so we know what version the reviewer saw.
func (b *Brain) SetFileReviewed(repo string, pr int, path, headSHA, baseSHA string) error {
	key := PRKey(repo, pr)
	tx, err := b.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`
		INSERT INTO file_reviews (pr_key, path, head_sha, base_sha, reviewed_at)
		VALUES (?, ?, ?, ?, datetime('now'))
		ON CONFLICT (pr_key, path) DO UPDATE SET head_sha = excluded.head_sha, base_sha = excluded.base_sha, reviewed_at = excluded.reviewed_at`,
		key, path, headSHA, baseSHA); err != nil {
		return err
	}
	payload := map[string]any{"head_sha": headSHA, "base_sha": baseSHA}
	if err := logEvent(tx, "file.reviewed", key, path, payload); err != nil {
		return err
	}
	return tx.Commit()
}

// FileReviewedState returns the head and base SHAs the reviewer last saw for
// this file. Returns zero FileReviewState if the file has never been reviewed.
func (b *Brain) FileReviewedState(repo string, pr int, path string) FileReviewState {
	key := PRKey(repo, pr)
	var s FileReviewState
	b.db.QueryRow(`SELECT head_sha, base_sha FROM file_reviews WHERE pr_key = ? AND path = ?`, key, path).Scan(&s.HeadSHA, &s.BaseSHA)
	return s
}

// AllFileReviewedStates returns every (path → FileReviewState) for a given PR.
func (b *Brain) AllFileReviewedStates(repo string, pr int) map[string]FileReviewState {
	key := PRKey(repo, pr)
	rows, err := b.db.Query(`SELECT path, head_sha, base_sha FROM file_reviews WHERE pr_key = ?`, key)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := map[string]FileReviewState{}
	for rows.Next() {
		var p string
		var s FileReviewState
		if rows.Scan(&p, &s.HeadSHA, &s.BaseSHA) == nil {
			out[p] = s
		}
	}
	return out
}

func (b *Brain) UnseenCount(repo string, pr int, files []gh.FileChange) int {
	n := 0
	for _, f := range files {
		if b.Status(repo, pr, f) != StatusSeen {
			n++
		}
	}
	return n
}
