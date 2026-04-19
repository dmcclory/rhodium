package main

import (
	"fmt"

	"github.com/charmbracelet/bubbles/list"
)

func (m model) viewTodo() string {
	return m.todo.View()
}

// rebuildTodoItems walks m.allPRs and emits a todoItem for each PR with
// outstanding work. Groups into two sections: "needs attention" (in-progress,
// catch-up, notes) and "new" (never-touched). When there's nothing actionable,
// shows a "caught up" header so the list isn't a wall of "unseen" rows with
// no context.
func (m *model) rebuildTodoItems() {
	var savedKey string
	if sel, ok := m.todo.SelectedItem().(todoItem); ok {
		savedKey = prKey(sel.pr.Repo, sel.pr.Number)
	}

	var actionable, newPRs []todoItem
	for _, pr := range m.allPRs {
		key := prKey(pr.Repo, pr.Number)
		ti := buildTodoItem(m, pr)

		// Pin PRs to "needs attention" once they first appear there — prevents
		// the list from shifting under the user as they mark things reviewed.
		isActionableNow := ti != nil && !(len(ti.tags) == 1 && ti.tags[0] == "unseen")
		if isActionableNow {
			m.pinnedAttention[key] = true
		}

		if m.pinnedAttention[key] {
			if ti == nil {
				// All work resolved mid-session — show as done rather than vanish.
				ti = &todoItem{pr: pr, tags: []string{"done"}}
			}
			actionable = append(actionable, *ti)
			continue
		}
		if ti == nil {
			continue
		}
		// Not pinned → must be the "unseen" case.
		newPRs = append(newPRs, *ti)
	}

	var items []list.Item
	switch {
	case len(actionable) > 0:
		items = append(items, sectionItem{label: "── needs attention ──"})
		for _, it := range actionable {
			items = append(items, it)
		}
		if len(newPRs) > 0 {
			items = append(items, sectionItem{label: "── new ──"})
			for _, it := range newPRs {
				items = append(items, it)
			}
		}
	case len(newPRs) > 0:
		items = append(items, sectionItem{label: "── ✓ caught up — new PRs below ──"})
		for _, it := range newPRs {
			items = append(items, it)
		}
	default:
		items = append(items, sectionItem{label: "── ✓ nothing to do ──"})
	}

	m.todo.SetItems(items)
	total := len(actionable) + len(newPRs)
	m.todo.Title = fmt.Sprintf("Todo (%d)", total)

	if savedKey != "" {
		for i, it := range items {
			if pi, ok := it.(todoItem); ok && prKey(pi.pr.Repo, pi.pr.Number) == savedKey {
				m.todo.Select(i)
				break
			}
		}
	}
}

// buildTodoItem returns a todoItem for pr if it needs attention, or nil otherwise.
func buildTodoItem(m *model, pr PR) *todoItem {
	notes := m.brain.NoteCountForPR(pr.Repo, pr.Number)
	cu := m.brain.ActiveCatchUp(pr.Repo, pr.Number)
	touched := m.brain.HasAnyMarks(pr.Repo, pr.Number) ||
		len(m.brain.AllFileReviewedStates(pr.Repo, pr.Number)) > 0

	files, filesLoaded := m.prFiles[prKey(pr.Repo, pr.Number)]
	var remaining int
	if filesLoaded {
		remaining = m.brain.UnseenCount(pr.Repo, pr.Number, files)
	}

	it := todoItem{pr: pr, notes: notes, remaining: remaining}
	// in-progress: reviewer has started. Drop it once every file is fully seen
	// and there's no catch-up pending — nothing left to do.
	if touched && cu == nil {
		if !filesLoaded || remaining > 0 {
			it.tags = append(it.tags, "in-progress")
		}
	}
	if cu != nil {
		it.tags = append(it.tags, "catch-up")
		it.done = cu.FilesDone
		it.total = cu.FilesTotal
	}
	if !touched && cu == nil {
		it.tags = append(it.tags, "unseen")
	}
	if notes > 0 {
		it.tags = append(it.tags, "notes")
	}
	if len(it.tags) == 0 {
		return nil
	}
	return &it
}
