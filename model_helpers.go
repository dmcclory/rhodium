package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
)

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
}

func (m *model) rebuildFileItems() {
	if m.selectedPR == nil {
		return
	}
	var savedPath string
	if sel, ok := m.files.SelectedItem().(fileItem); ok {
		savedPath = sel.fc.Path
	}
	files := m.prFiles[prKey(m.selectedPR.Repo, m.selectedPR.Number)]
	reviewedStates := m.brain.AllFileReviewedStates(m.selectedPR.Repo, m.selectedPR.Number)
	var unseen, partial, seen []fileItem
	for _, fc := range files {
		status := m.brain.Status(m.selectedPR.Repo, m.selectedPR.Number, fc)
		nc := m.brain.NoteCountForFile(m.selectedPR.Repo, m.selectedPR.Number, fc.Path)
		s := reviewedStates[fc.Path]
		catchUp := s.HeadSHA != "" && (s.HeadSHA != m.selectedPR.HeadSHA || s.BaseSHA != m.selectedPR.BaseSHA)
		fi := fileItem{fc: fc, status: status, noteCount: nc, needsCatchUp: catchUp}
		switch status {
		case StatusUnseen:
			unseen = append(unseen, fi)
		case StatusPartial:
			partial = append(partial, fi)
		case StatusSeen:
			seen = append(seen, fi)
		}
	}
	var items []list.Item
	needSep := false
	if len(partial) > 0 {
		items = append(items, sectionItem{label: "── in progress ──"})
		for _, fi := range partial {
			items = append(items, fi)
		}
		needSep = true
	}
	if len(unseen) > 0 {
		if needSep {
			items = append(items, sectionItem{label: "── unseen ──"})
		}
		for _, fi := range unseen {
			items = append(items, fi)
		}
		needSep = true
	}
	if len(seen) > 0 {
		if needSep {
			items = append(items, sectionItem{label: "── seen ──"})
		}
		for _, fi := range seen {
			items = append(items, fi)
		}
	}
	m.files.SetItems(items)
	if savedPath != "" {
		for i, it := range items {
			if fi, ok := it.(fileItem); ok && fi.fc.Path == savedPath {
				m.files.Select(i)
				break
			}
		}
	}
}

var (
	tabActiveStyle   = lipgloss.NewStyle().Bold(true).Underline(true)
	tabInactiveStyle = lipgloss.NewStyle().Faint(true)
)

func (m *model) tabBar() string {
	tabs := []struct {
		label string
		t     fileTab
	}{
		{"[1] Files", tabFiles},
		{"[2] Description", tabDescription},
		{"[3] Notes", tabNotes},
	}
	var parts []string
	for _, tab := range tabs {
		if tab.t == m.fileTab {
			parts = append(parts, tabActiveStyle.Render(tab.label))
		} else {
			parts = append(parts, tabInactiveStyle.Render(tab.label))
		}
	}
	return strings.Join(parts, "  ") + "\n"
}

func (m *model) rebuildInfoVP() {
	if m.selectedPR == nil {
		return
	}
	var content string
	switch m.fileTab {
	case tabDescription:
		body := m.selectedPR.Body
		if body == "" {
			body = "(no description)"
		}
		content = fmt.Sprintf("%s#%d  %s  @%s\n\n%s",
			m.selectedPR.Repo, m.selectedPR.Number, m.selectedPR.Title, m.selectedPR.Author, body)
	case tabNotes:
		notes := m.brain.NotesForPR(m.selectedPR.Repo, m.selectedPR.Number)
		if len(notes) == 0 {
			content = "(no notes)"
		} else {
			key := prKey(m.selectedPR.Repo, m.selectedPR.Number)
			fileLinesCache := map[string][]string{}
			getFileLines := func(path string) []string {
				if cached, ok := fileLinesCache[path]; ok {
					return cached
				}
				lines := m.patchNewFileLines(key, path)
				fileLinesCache[path] = lines
				return lines
			}

			var b strings.Builder
			curPath := ""
			for _, n := range notes {
				if n.Path != curPath {
					if curPath != "" {
						b.WriteByte('\n')
					}
					curPath = n.Path
					b.WriteString(lipgloss.NewStyle().Bold(true).Render(curPath) + "\n")
				}
				// Context lines around the note.
				fLines := getFileLines(n.Path)
				idx := n.LineNo - 1
				ctxStart := idx - 2
				if ctxStart < 0 {
					ctxStart = 0
				}
				ctxEnd := idx + 3
				if ctxEnd > len(fLines) {
					ctxEnd = len(fLines)
				}
				for i := ctxStart; i < ctxEnd; i++ {
					lineStr := fmt.Sprintf("  %4d  %s", i+1, fLines[i])
					if i == idx {
						lineStr = lipgloss.NewStyle().Bold(true).Render(lineStr)
					} else {
						lineStr = lipgloss.NewStyle().Faint(true).Render(lineStr)
					}
					b.WriteString(lineStr + "\n")
				}
				b.WriteString(noteStyle.Render("  "+strings.Repeat(" ", 4)+"  RH: "+n.Body) + "\n")
			}
			content = b.String()
		}
	}
	m.infoVP.SetContent(content)
	m.infoVP.GotoTop()
}

// patchNewFileLines reconstructs the new-file lines visible in a patch's hunks.
// Returns a sparse slice indexed by 1-based line number. Lines not covered by
// any hunk are empty strings (best effort — we may not have the full file).
func (m *model) patchNewFileLines(key, path string) []string {
	files := m.prFiles[key]
	var patch string
	for _, f := range files {
		if f.Path == path {
			patch = f.Patch
			break
		}
	}
	if patch == "" {
		return nil
	}
	hunks := parseHunks(patch)
	// Find max line to size the slice.
	maxLine := 0
	for _, h := range hunks {
		r := parseHunkRange(h.Header)
		end := r.newStart + r.newCount
		if end > maxLine {
			maxLine = end
		}
	}
	lines := make([]string, maxLine+1)
	for _, h := range hunks {
		r := parseHunkRange(h.Header)
		cur := r.newStart
		for _, line := range h.BodyLines {
			if len(line) == 0 {
				if cur < len(lines) {
					lines[cur] = ""
				}
				cur++
				continue
			}
			switch line[0] {
			case '-':
				// deleted from old file, not in new
			case '+':
				if cur < len(lines) {
					lines[cur] = line[1:]
				}
				cur++
			default:
				if cur < len(lines) {
					text := line
					if len(text) > 0 && text[0] == ' ' {
						text = text[1:]
					}
					lines[cur] = text
				}
				cur++
			}
		}
	}
	return lines
}

func (m *model) currentFile() (FileChange, bool) {
	if m.selectedPR == nil {
		return FileChange{}, false
	}
	for _, f := range m.prFiles[prKey(m.selectedPR.Repo, m.selectedPR.Number)] {
		if f.Path == m.selectedFile {
			return f, true
		}
	}
	return FileChange{}, false
}

func firstUnmarked(hunks []Hunk, marks map[string]bool) int {
	for i, h := range hunks {
		if !marks[h.Hash] {
			return i
		}
	}
	return 0
}

func hashLine(s string) string {
	h := hashHunkBody([]string{"+" + s})
	return h
}

// advanceCatchUpSession advances the active catch-up session by one file.
func (m *model) advanceCatchUpSession() {
	if m.catchUpSession != nil {
		m.brain.CatchUpAdvanceFile(m.catchUpSession.ID)
		m.catchUpSession = m.brain.ActiveCatchUp(m.selectedPR.Repo, m.selectedPR.Number)
	}
}

func (m *model) saveMarks() {
	if m.selectedPR == nil || m.selectedFile == "" {
		return
	}
	if err := m.brain.SetHunkMarks(m.selectedPR.Repo, m.selectedPR.Number, m.selectedFile, m.currentMarks); err != nil {
		m.statusMsg = "save error: " + err.Error()
		return
	}
	// Record the PR head SHA we're reviewing against so catch-up diffs
	// know what version we last saw.
	if m.selectedPR.HeadSHA != "" {
		m.brain.SetFileReviewed(m.selectedPR.Repo, m.selectedPR.Number, m.selectedFile, m.selectedPR.HeadSHA, m.selectedPR.BaseSHA)
	}
}
