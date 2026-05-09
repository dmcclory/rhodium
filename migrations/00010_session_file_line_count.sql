-- +goose Up
-- Add line_count to review_session_files so sessions can track progress by
-- lines remaining rather than just files-done. This gives a more honest
-- progress signal when file sizes vary wildly (e.g. 9/10 done but the last
-- one has 800 lines). line_count defaults to 0 for backwards-compat with
-- pre-existing sessions.
ALTER TABLE review_session_files ADD COLUMN line_count INTEGER NOT NULL DEFAULT 0;

-- +goose Down
-- SQLite doesn't support DROP COLUMN in older versions; this is best-effort.
-- The column is harmless if left in place.
