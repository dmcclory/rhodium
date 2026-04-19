package main

import (
	"fmt"

	"github.com/charmbracelet/bubbles/list"
)

func (m model) viewPRs() string {
	return m.prs.View()
}

// mergePRs appends PRs whose (repo, number) aren't already in m.allPRs and
// returns just the newly-added ones, so callers can kick off file prefetch
// without redundantly re-fetching PRs already loaded.
func (m *model) mergePRs(prs []PR) []PR {
	seen := make(map[string]bool, len(m.allPRs))
	for _, p := range m.allPRs {
		seen[prKey(p.Repo, p.Number)] = true
	}
	var added []PR
	for _, p := range prs {
		k := prKey(p.Repo, p.Number)
		if seen[k] {
			continue
		}
		seen[k] = true
		m.allPRs = append(m.allPRs, p)
		added = append(added, p)
	}
	return added
}

func (m *model) rebuildPRItems() {
	// Remember which PR the cursor is on so rebuild doesn't jump it.
	var savedKey string
	if sel, ok := m.prs.SelectedItem().(prItem); ok {
		savedKey = prKey(sel.pr.Repo, sel.pr.Number)
	}

	var inProgress, untouched []prItem
	for _, pr := range m.allPRs {
		it := prItem{pr: pr, noteCount: m.brain.NoteCountForPR(pr.Repo, pr.Number), scrutinized: m.brain.IsScrutinized(pr.Repo, pr.Number)}
		// A PR is "in progress" if the brain has any marks for it, even
		// before we've fetched its file list. This keeps already-touched
		// PRs from popping between buckets during startup prefetch.
		looked := m.brain.HasAnyMarks(pr.Repo, pr.Number)
		if files, ok := m.prFiles[prKey(pr.Repo, pr.Number)]; ok {
			unseen := m.brain.UnseenCount(pr.Repo, pr.Number, files)
			if unseen == 0 {
				it.summary = "✓ caught up"
			} else {
				it.summary = fmt.Sprintf("%d new", unseen)
			}
			// Show catch-up session progress if one exists.
			if session := m.brain.ActiveCatchUp(pr.Repo, pr.Number); session != nil {
				remaining := session.FilesTotal - session.FilesDone
				it.summary += fmt.Sprintf(", ↻ %d/%d", session.FilesDone, session.FilesTotal)
				_ = remaining
			} else {
				// No session yet — count files needing catch-up.
				reviewedStates := m.brain.AllFileReviewedStates(pr.Repo, pr.Number)
				catchUpCount := 0
				for _, f := range files {
					if s := reviewedStates[f.Path]; s.HeadSHA != "" && (s.HeadSHA != pr.HeadSHA || s.BaseSHA != pr.BaseSHA) {
						catchUpCount++
					}
				}
				if catchUpCount > 0 {
					it.summary += fmt.Sprintf(", %d ↻", catchUpCount)
				}
			}
		}
		if looked {
			inProgress = append(inProgress, it)
		} else {
			untouched = append(untouched, it)
		}
	}

	var items []list.Item
	if len(inProgress) > 0 {
		items = append(items, sectionItem{label: "── in progress ──"})
		for _, it := range inProgress {
			items = append(items, it)
		}
	}
	if len(untouched) > 0 {
		if len(inProgress) > 0 {
			items = append(items, sectionItem{label: "── new ──"})
		}
		for _, it := range untouched {
			items = append(items, it)
		}
	}
	m.prs.SetItems(items)
	if savedKey != "" {
		for i, it := range items {
			if pi, ok := it.(prItem); ok && prKey(pi.pr.Repo, pi.pr.Number) == savedKey {
				m.prs.Select(i)
				break
			}
		}
	}
	// Todo list is a filtered view over the same data — rebuild in lockstep.
	m.rebuildTodoItems()
}
