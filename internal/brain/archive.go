package brain

import (
	"fmt"
)

// ArchivePR removes a PR from pr_cache and records a tombstone in
// archived_prs. All brain data (notes, hunk_marks, file_reviews, sessions)
// is preserved so replay and brain log continue to work. Active sessions
// are completed first.
func (b *Brain) ArchivePR(repo string, number int, reason string) error {
	key := PRKey(repo, number)

	// Validate reason
	switch reason {
	case "merged", "closed", "manual":
	default:
		return fmt.Errorf("archive reason must be merged, closed, or manual, got %q", reason)
	}

	tx, err := b.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Complete any active sessions for this PR.
	if _, err := tx.Exec(
		`UPDATE review_sessions SET completed_at = datetime('now') WHERE pr_key = ? AND completed_at IS NULL`, key,
	); err != nil {
		return fmt.Errorf("complete sessions: %w", err)
	}

	// Remove from pr_cache.
	if _, err := tx.Exec(`DELETE FROM pr_cache WHERE repo = ? AND number = ?`, repo, number); err != nil {
		return fmt.Errorf("delete from cache: %w", err)
	}

	// Insert tombstone (upsert so re-archiving is idempotent).
	if _, err := tx.Exec(
		`INSERT INTO archived_prs (pr_key, reason) VALUES (?, ?)
		 ON CONFLICT (pr_key) DO UPDATE SET reason = excluded.reason`,
		key, reason); err != nil {
		return fmt.Errorf("insert tombstone: %w", err)
	}

	// Log the archive event.
	if err := logEvent(tx, "brain.archive", key, "", map[string]any{
		"reason": reason,
	}); err != nil {
		return fmt.Errorf("log event: %w", err)
	}

	return tx.Commit()
}

// IsArchived returns true if the PR has been archived.
func (b *Brain) IsArchived(repo string, number int) bool {
	key := PRKey(repo, number)
	var exists bool
	if err := b.db.QueryRow(`SELECT 1 FROM archived_prs WHERE pr_key = ?`, key).Scan(&exists); err != nil {
		return false
	}
	return true
}

// ArchivedEntry is one row from the archived_prs table.
type ArchivedEntry struct {
	PRKey      string `json:"pr_key"`
	ArchivedAt string `json:"archived_at"`
	Reason     string `json:"reason"`
}

// ListArchived returns all archived PRs, newest first.
func (b *Brain) ListArchived() []ArchivedEntry {
	rows, err := b.db.Query(`SELECT pr_key, archived_at, reason FROM archived_prs ORDER BY archived_at DESC`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []ArchivedEntry
	for rows.Next() {
		var e ArchivedEntry
		if rows.Scan(&e.PRKey, &e.ArchivedAt, &e.Reason) == nil {
			out = append(out, e)
		}
	}
	return out
}

// Unarchive removes a PR from the archived list. The PR will reappear in
// lists on the next `--sync`. Brain data is untouched — this just undoes
// the tombstone so the PR can be re-cached.
func (b *Brain) Unarchive(repo string, number int) error {
	key := PRKey(repo, number)
	_, err := b.db.Exec(`DELETE FROM archived_prs WHERE pr_key = ?`, key)
	return err
}
