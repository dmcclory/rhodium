-- +goose Up
-- Store line_count per hunk mark so session progress can track lines
-- reviewed incrementally as individual hunks are marked, not just when
-- entire files are done.
ALTER TABLE hunk_marks ADD COLUMN line_count INTEGER NOT NULL DEFAULT 0;

-- +goose Down
-- SQLite doesn't support DROP COLUMN in older versions; harmless to leave.
