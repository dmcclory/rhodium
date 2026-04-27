package brain

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// Event is a single row from brain_events: one mutation of brain state.
// Payload stays as raw JSON here; callers that care about the shape
// per-kind unmarshal it themselves.
type Event struct {
	ID      int64
	TS      string
	Kind    string
	PRKey   string
	Path    string
	Payload string
}

// EventFilter narrows RecentEvents. Zero-value fields are ignored, so
// an empty filter returns the most recent events across the whole brain.
// KindPrefix matches via SQL LIKE so "mark." grabs both mark.set and
// mark.clear. Limit <= 0 defaults to 100 — the log is append-only and
// can grow without bound, so callers should always page.
type EventFilter struct {
	PRKey      string
	KindPrefix string
	Limit      int
}

// execer is the shared subset of *sql.DB and *sql.Tx that logEvent needs,
// so mutators already holding a transaction can write the state change
// and its event atomically.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// logEvent appends one row to brain_events. Callers inside a transaction
// must pass that tx as x so the event shares the state write's atomicity
// — if the tx rolls back, no orphan event survives. Payload is marshalled
// to JSON; a nil payload becomes "{}".
func logEvent(x execer, kind, prKey, path string, payload any) error {
	var body string
	if payload == nil {
		body = "{}"
	} else {
		buf, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal event payload: %w", err)
		}
		body = string(buf)
	}
	_, err := x.Exec(
		`INSERT INTO brain_events (kind, pr_key, path, payload) VALUES (?, ?, ?, ?)`,
		kind, prKey, path, body)
	return err
}

// RecentEvents returns events in reverse-chronological order (newest
// first) subject to the filter. Intended for a future `rhodium brain
// log` CLI; exposed now so tests can assert the log is being populated.
func (b *Brain) RecentEvents(filter EventFilter) []Event {
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	var (
		where []string
		args  []any
	)
	if filter.PRKey != "" {
		where = append(where, `pr_key = ?`)
		args = append(args, filter.PRKey)
	}
	if filter.KindPrefix != "" {
		where = append(where, `kind LIKE ?`)
		args = append(args, filter.KindPrefix+"%")
	}
	q := `SELECT id, ts, kind, pr_key, path, payload FROM brain_events`
	if len(where) > 0 {
		q += ` WHERE ` + strings.Join(where, ` AND `)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := b.db.Query(q, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		if rows.Scan(&e.ID, &e.TS, &e.Kind, &e.PRKey, &e.Path, &e.Payload) == nil {
			out = append(out, e)
		}
	}
	return out
}
