-- +goose Up

-- Track WHY a file was marked reviewed: "user" means the reviewer actually
-- opened it, "auto" means it was auto-advanced (rev-update, no hunks, all
-- hunks already marked, or mark-fully-reviewed). Enables the UI to surface
-- "this was auto-applied; you might want to look anyway" for trust and
-- debugging.
ALTER TABLE file_reviews ADD COLUMN mark_kind TEXT NOT NULL DEFAULT 'user';

-- +goose Down
-- SQLite supports DROP COLUMN from 3.35.0 onward.
ALTER TABLE file_reviews DROP COLUMN mark_kind;
