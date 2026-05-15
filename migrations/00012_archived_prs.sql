-- +goose Up
-- archived_prs is a tombstone table. When a PR is archived, its brain data
-- (notes, hunk_marks, file_reviews, sessions) is preserved so brain log /
-- replay still work, but the PR is removed from pr_cache so it stops
-- cluttering the todo and PR lists.
CREATE TABLE archived_prs (
    pr_key      TEXT NOT NULL PRIMARY KEY,
    archived_at TEXT NOT NULL DEFAULT (datetime('now')),
    reason      TEXT -- "merged", "closed", "manual"
);

-- +goose Down
DROP TABLE archived_prs;
