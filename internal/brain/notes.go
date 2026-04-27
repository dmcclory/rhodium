package brain

import (
	"database/sql"
	"fmt"
)

type Note struct {
	ID              int64  `json:"id"`
	PRKey           string `json:"pr_key"`
	Path            string `json:"path"`
	LineNo          int    `json:"line_no"`
	LineHash        string `json:"line_hash"`
	Body            string `json:"body"`
	Source          string `json:"source"` // "human" (typed via `c`) or "agent" (first-pass review)
	CreatedAt       string `json:"created_at"`
	ResolvedAt      string `json:"resolved_at,omitempty"`
	GitHubCommentID int64  `json:"github_comment_id,omitempty"` // 0 = local only; else the id GitHub returned
}

// NoteFilter controls whether NotesForPR / NotesForFile / PRKeysWithNotes
// include resolved notes. Counts always reflect Active-only so resolved
// notes drop out of the todo dashboard.
type NoteFilter int

const (
	NotesActive NoteFilter = iota // resolved_at IS NULL
	NotesAll                      // active + resolved
)

// PRKeysWithNotes returns every pr_key that has at least one active note, sorted.
func (b *Brain) PRKeysWithNotes() []string {
	rows, err := b.db.Query(`SELECT DISTINCT pr_key FROM notes WHERE resolved_at IS NULL`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if rows.Scan(&k) == nil {
			out = append(out, k)
		}
	}
	return out
}

func (b *Brain) NoteCountForPR(repo string, pr int) int {
	key := PRKey(repo, pr)
	var count int
	b.db.QueryRow(`SELECT COUNT(*) FROM notes WHERE pr_key = ? AND resolved_at IS NULL`, key).Scan(&count)
	return count
}

func (b *Brain) NoteCountForFile(repo string, pr int, path string) int {
	key := PRKey(repo, pr)
	var count int
	b.db.QueryRow(`SELECT COUNT(*) FROM notes WHERE pr_key = ? AND path = ? AND resolved_at IS NULL`, key, path).Scan(&count)
	return count
}

func (b *Brain) NotesForPR(repo string, pr int, filter NoteFilter) []Note {
	key := PRKey(repo, pr)
	q := `SELECT id, pr_key, path, line_no, line_hash, body, source, created_at, resolved_at, github_comment_id FROM notes WHERE pr_key = ?`
	if filter == NotesActive {
		q += ` AND resolved_at IS NULL`
	}
	q += ` ORDER BY path, line_no, id`
	rows, err := b.db.Query(q, key)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Note
	for rows.Next() {
		var n Note
		var resolved sql.NullString
		var ghID sql.NullInt64
		if rows.Scan(&n.ID, &n.PRKey, &n.Path, &n.LineNo, &n.LineHash, &n.Body, &n.Source, &n.CreatedAt, &resolved, &ghID) == nil {
			if resolved.Valid {
				n.ResolvedAt = resolved.String
			}
			if ghID.Valid {
				n.GitHubCommentID = ghID.Int64
			}
			out = append(out, n)
		}
	}
	return out
}

func (b *Brain) NotesForFile(repo string, pr int, path string) []Note {
	key := PRKey(repo, pr)
	rows, err := b.db.Query(
		`SELECT id, pr_key, path, line_no, line_hash, body, source, created_at, resolved_at, github_comment_id
		 FROM notes WHERE pr_key = ? AND path = ? AND resolved_at IS NULL ORDER BY line_no, id`,
		key, path)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Note
	for rows.Next() {
		var n Note
		var resolved sql.NullString
		var ghID sql.NullInt64
		if rows.Scan(&n.ID, &n.PRKey, &n.Path, &n.LineNo, &n.LineHash, &n.Body, &n.Source, &n.CreatedAt, &resolved, &ghID) == nil {
			if resolved.Valid {
				n.ResolvedAt = resolved.String
			}
			if ghID.Valid {
				n.GitHubCommentID = ghID.Int64
			}
			out = append(out, n)
		}
	}
	return out
}

func (b *Brain) SaveNote(repo string, pr int, path string, lineNo int, lineHash, body string) error {
	return b.insertNote(PRKey(repo, pr), path, lineNo, lineHash, body, "human")
}

// insertNote is the shared path for human and agent notes: one tx that
// writes the row and the note.add event. The event payload carries the
// full body so a future replay-from-events can reconstruct the note even
// if the row has since been hard-deleted.
func (b *Brain) insertNote(key, path string, lineNo int, lineHash, body, source string) error {
	tx, err := b.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(
		`INSERT INTO notes (pr_key, path, line_no, line_hash, body, source) VALUES (?, ?, ?, ?, ?, ?)`,
		key, path, lineNo, lineHash, body, source)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	payload := map[string]any{
		"note_id":   id,
		"line_no":   lineNo,
		"line_hash": lineHash,
		"source":    source,
		"body":      body,
	}
	if err := logEvent(tx, "note.add", key, path, payload); err != nil {
		return err
	}
	return tx.Commit()
}

// SaveAgentNote records a note produced by an inline-notes action. Agents
// don't see per-line hashes so line_hash stays empty; source="agent" keeps
// these filterable away from human notes in future UI work.
func (b *Brain) SaveAgentNote(repo string, pr int, path string, lineNo int, body string) error {
	return b.insertNote(PRKey(repo, pr), path, lineNo, "", body, "agent")
}

// SetNoteGitHubCommentID stamps the id GitHub returned on the note, so we
// know the note has been published and can skip it on re-publish. Idempotent:
// re-stamping the same id is a no-op and emits no event.
func (b *Brain) SetNoteGitHubCommentID(id, ghID int64) error {
	tx, err := b.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var key, path string
	var existing sql.NullInt64
	switch err = tx.QueryRow(
		`SELECT pr_key, path, github_comment_id FROM notes WHERE id = ?`, id,
	).Scan(&key, &path, &existing); err {
	case sql.ErrNoRows:
		return fmt.Errorf("note %d not found", id)
	case nil:
	default:
		return err
	}
	if existing.Valid && existing.Int64 == ghID {
		return nil
	}
	if _, err := tx.Exec(`UPDATE notes SET github_comment_id = ? WHERE id = ?`, ghID, id); err != nil {
		return err
	}
	if err := logEvent(tx, "note.publish", key, path, map[string]any{
		"note_id":           id,
		"github_comment_id": ghID,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// ResolveNote marks a note as resolved (soft delete — the row stays so
// `rhodium notes --all` can show history). Idempotent: resolving an
// already-resolved or missing note is a no-op and emits no event.
func (b *Brain) ResolveNote(id int64) error {
	tx, err := b.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var key, path string
	var resolved sql.NullString
	switch err = tx.QueryRow(
		`SELECT pr_key, path, resolved_at FROM notes WHERE id = ?`, id,
	).Scan(&key, &path, &resolved); err {
	case sql.ErrNoRows:
		return nil
	case nil:
	default:
		return err
	}
	if resolved.Valid {
		return nil
	}
	if _, err := tx.Exec(`UPDATE notes SET resolved_at = datetime('now') WHERE id = ?`, id); err != nil {
		return err
	}
	if err := logEvent(tx, "note.resolve", key, path, map[string]any{"note_id": id}); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteNote hard-deletes a note row. The event payload captures the
// deleted row's contents (body, line anchor, source, resolved_at) so a
// future undo/replay can resurrect the note without any other source.
func (b *Brain) DeleteNote(id int64) error {
	tx, err := b.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var (
		key, path, body, source, lineHash string
		lineNo                            int
		resolved                          sql.NullString
	)
	switch err = tx.QueryRow(
		`SELECT pr_key, path, line_no, line_hash, body, source, resolved_at FROM notes WHERE id = ?`, id,
	).Scan(&key, &path, &lineNo, &lineHash, &body, &source, &resolved); err {
	case sql.ErrNoRows:
		return nil
	case nil:
	default:
		return err
	}
	if _, err := tx.Exec(`DELETE FROM notes WHERE id = ?`, id); err != nil {
		return err
	}
	payload := map[string]any{
		"note_id":   id,
		"line_no":   lineNo,
		"line_hash": lineHash,
		"source":    source,
		"body":      body,
	}
	if resolved.Valid {
		payload["resolved_at"] = resolved.String
	}
	if err := logEvent(tx, "note.delete", key, path, payload); err != nil {
		return err
	}
	return tx.Commit()
}
