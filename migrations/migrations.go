// Package migrations exposes the embedded brain DB migration files. Lives
// at repo root so the goose CLI can find a stable directory path; the
// embed wrapper lets brain (or any other package) consume the .sql files
// without a relative-path dance.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
