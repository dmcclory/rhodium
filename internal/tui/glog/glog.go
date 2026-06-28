// Package glog is the TUI commit-log-with-review-overlay view (the "glog"
// lens). It mirrors the diff view's custom scroll renderer: collapsed commit
// rows that expand inline to their hunks. The per-commit review badges come
// from rhodium/internal/glog.Rollup.
package glog

import (
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"rhodium/internal/gh"
	coreglog "rhodium/internal/glog"
	"rhodium/internal/tui/keys"
	"rhodium/internal/tui/router"
)

type Model struct {
	vp       viewport.Model
	pr       *gh.PR
	commits  []coreglog.CommitRollup
	cursor   int          // index of the focused commit
	expanded map[int]bool // commit indices shown with their files→hunks tree
	width    int
	height   int

	// BackRoute is where `back` (esc/h) returns — the list the PR was
	// opened from (todo or prs). Set by the app on entry.
	BackRoute router.Route
}

func New() Model {
	return Model{vp: viewport.New(0, 0), expanded: map[int]bool{}}
}

func (m *Model) Resize(w, h int) {
	m.vp.Width = w
	m.vp.Height = h
	m.width = w
	m.height = h
}

// SetCommits replaces the displayed commit rollups and redraws. Used by the
// app once ListPRCommits + FetchCommitFiles + Rollup have completed.
func (m *Model) SetCommits(pr *gh.PR, commits []coreglog.CommitRollup) {
	m.pr = pr
	m.commits = commits
	// Default to fully expanded — a review pass wants every commit's hunks
	// visible at once; `enter` collapses the ones you've cleared.
	m.expanded = make(map[int]bool, len(commits))
	for i := range commits {
		m.expanded[i] = true
	}
	if m.cursor >= len(commits) {
		m.cursor = 0
	}
	m.redraw()
}

func (m *Model) PR() *gh.PR { return m.pr }

func (m *Model) View() string { return m.vp.View() }

func (m *Model) Footer() string {
	return "glog · ↑/↓: commit  enter: expand  g: files  esc: back"
}

// Update handles cursor movement directly, routes other keys through this
// view's bindings + the app globals, and delegates the rest to the viewport.
func (m *Model) Update(msg tea.Msg, globals []keys.Binding) tea.Cmd {
	key, isKey := msg.(tea.KeyMsg)
	if !isKey {
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return cmd
	}
	switch key.String() {
	case "up", "k":
		m.moveCursor(-1)
		return nil
	case "down", "j":
		m.moveCursor(1)
		return nil
	}
	if cmd, matched := keys.Dispatch(key.String(), false, m.Bindings(), globals); matched {
		return cmd
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return cmd
}

// Bindings are this view's keys: expand/collapse the focused commit, back to
// the originating list, and the `g` lens toggle to the files view.
func (m *Model) Bindings() []keys.Binding {
	return []keys.Binding{
		{
			Name: "expand", Keys: []string{"enter", "l", "right"},
			Desc: "expand/collapse commit", Group: "View",
			Action: func() tea.Cmd { m.toggleExpand(); return nil },
		},
		{
			Name: "back", Keys: []string{"esc", "h", "left"},
			Desc: "back", Group: "Navigate",
			Action: func() tea.Cmd { return router.Navigate(m.BackRoute) },
		},
		{
			Name: "files", Keys: []string{"g"},
			Desc: "files view", Group: "View",
			Action: func() tea.Cmd { return router.Navigate(router.RouteFiles) },
		},
	}
}

func (m *Model) toggleExpand() {
	if len(m.commits) == 0 {
		return
	}
	m.expanded[m.cursor] = !m.expanded[m.cursor]
	m.redraw()
}

func (m *Model) moveCursor(delta int) {
	if len(m.commits) == 0 {
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.commits) {
		m.cursor = len(m.commits) - 1
	}
	m.redraw()
}

func (m *Model) redraw() {
	m.vp.SetContent(renderCommits(m.pr, m.commits, m.cursor, m.expanded))
}
