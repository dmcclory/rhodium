package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// program is a package-level handle to the running tea program, set in main
// before Run(). Goroutines spawned from tea.Cmd's use it to push messages
// back onto the update loop.
var program *tea.Program

// --- model ---

type model struct {
	cfg    *Config
	brain  *Brain
	view   view
	width  int
	height int

	prs   list.Model
	files list.Model
	diff  viewport.Model

	fileTab      fileTab
	infoVP       viewport.Model // for description / notes tabs

	allPRs       []PR
	freshKeys    map[string]bool         // keys confirmed still open by a repo listing
	prFiles      map[string][]FileChange // prKey → files
	selectedPR   *PR
	selectedFile string

	// Diff view state.
	currentHunks []Hunk
	currentMarks map[string]bool
	currentNotes []Note
	hunkLines    []int
	lineMap      []int // output line → new-file line number (0 = non-file line)
	hunkIdx      int
	cursorLine   int      // output line the cursor is on
	diffLines    []string // raw content lines for manual rendering in noting mode
	blobContent  string   // full file content, empty until blob loads

	// Catch-up diff state.
	catchUpMode     bool             // true when showing only the delta since last review
	catchUpOldHead  string           // the head SHA we last reviewed at (f1)
	catchUpOldBase  string           // the base SHA we last reviewed at (b1)
	catchUpClass    Class            // diff4 classification of the catch-up
	catchUpPatch    string           // the delta patch for the current file
	catchUpSession  *CatchUpSession  // active catch-up session for current PR

	// Note input state.
	noting       bool
	noteLineNo   int
	noteLineHash string
	noteInput    textarea.Model

	loadingFiles bool
	statusMsg    string
}

func compactDelegate() list.DefaultDelegate {
	d := list.NewDefaultDelegate()
	d.ShowDescription = false
	d.SetSpacing(0)
	return d
}

func newModel(cfg *Config, brain *Brain) model {
	prList := list.New(nil, compactDelegate(), 0, 0)

	fileList := list.New(nil, compactDelegate(), 0, 0)
	fileList.Title = "Files"

	vp := viewport.New(0, 0)
	infoVP := viewport.New(0, 0)

	ti := textarea.New()
	ti.Placeholder = "Write a note... (ctrl+d to save, esc to cancel)"
	ti.SetHeight(3)
	ti.ShowLineNumbers = false

	m := model{
		cfg:     cfg,
		brain:   brain,
		view:    viewPRs,
		prs:     prList,
		files:   fileList,
		diff:    vp,
		noteInput: ti,
		infoVP:    infoVP,
		prFiles:   map[string][]FileChange{},
		freshKeys: map[string]bool{},
	}

	cached := brain.CachedPRs()
	if len(cached) > 0 {
		m.allPRs = cached
		m.rebuildPRItems()
		m.prs.Title = fmt.Sprintf("PRs (%d, refreshing…)", len(cached))
	} else {
		m.prs.Title = "PRs (loading...)"
	}

	return m
}

func (m model) Init() tea.Cmd {
	cmds := make([]tea.Cmd, len(m.cfg.Repos))
	for i, repo := range m.cfg.Repos {
		cmds[i] = loadRepoPRsCmd(repo)
	}
	return tea.Batch(cmds...)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		h, v := appStyle.GetFrameSize()
		listW, listH := msg.Width-h, msg.Height-v-1
		m.prs.SetSize(listW, listH)
		m.files.SetSize(listW, listH)
		m.diff.Width = listW
		m.diff.Height = listH
		m.infoVP.Width = listW
		m.infoVP.Height = listH
		return m, nil

	case prsLoadedMsg:
		if msg.err != nil {
			m.statusMsg = "error: " + msg.err.Error()
			return m, nil
		}
		for _, p := range msg.prs {
			m.freshKeys[prKey(p.Repo, p.Number)] = true
		}
		added := m.mergePRs(msg.prs)
		m.rebuildPRItems()
		m.prs.Title = fmt.Sprintf("PRs (%d, loading files…)", len(m.allPRs))
		go m.brain.SetPRCache(m.allPRs)
		return m, prefetchAllCmd(added)

	case filesLoadedMsg:
		m.loadingFiles = false
		if msg.err != nil {
			m.statusMsg = "error: " + msg.err.Error()
			return m, nil
		}
		key := prKey(msg.pr.Repo, msg.pr.Number)
		m.prFiles[key] = msg.files
		m.rebuildPRItems()
		if m.selectedPR != nil && prKey(m.selectedPR.Repo, m.selectedPR.Number) == key {
			m.rebuildFileItems()
			m.files.Title = fmt.Sprintf("Files in %s#%d", msg.pr.Repo, msg.pr.Number)
		}
		// Kick off auto-advance for files that haven't changed since last review.
		// Skip for scrutinized PRs — those always need full review.
		pr := msg.pr
		files := msg.files
		if m.brain.IsScrutinized(pr.Repo, pr.Number) {
			return m, nil
		}
		return m, autoAdvanceCmd(m.brain, pr, files)

	case autoAdvanceMsg:
		if len(msg.advancedFiles) > 0 {
			m.rebuildPRItems()
			if m.selectedPR != nil && prKey(m.selectedPR.Repo, m.selectedPR.Number) == msg.prKey {
				m.rebuildFileItems()
				m.catchUpSession = m.brain.ActiveCatchUp(m.selectedPR.Repo, m.selectedPR.Number)
			}
			m.statusMsg = fmt.Sprintf("✓ auto-caught-up %d files", len(msg.advancedFiles))
		}
		return m, nil

	case catchUpLoadedMsg:
		if msg.err != nil {
			m.statusMsg = "catch-up: " + msg.err.Error()
			return m, nil
		}
		if m.view != viewDiff || m.selectedFile != msg.path {
			return m, nil
		}
		// No-rebase fast path: b1==b2, classify based on whether file changed.
		var deltaFC *FileChange
		for _, f := range msg.files {
			if f.Path == msg.path {
				deltaFC = &f
				break
			}
		}
		if deltaFC == nil || deltaFC.Patch == "" {
			// f1==f2 with b1==b2 → ClassB1B2__F1F2 (hidden, clean merge).
			m.catchUpMode = false
			m.catchUpClass = ClassB1B2__F1F2
			m.statusMsg = fmt.Sprintf("✓ %s: %s (auto-caught-up)", m.selectedFile, ClassB1B2__F1F2)
			m.brain.SetFileReviewed(m.selectedPR.Repo, m.selectedPR.Number, m.selectedFile, m.selectedPR.HeadSHA, m.selectedPR.BaseSHA)
			m.advanceCatchUpSession()
			return m, nil
		}
		// b1==b2, f1≠f2 → ClassB1B2 ("diff extension"), show f1→f2.
		m.catchUpMode = true
		m.catchUpClass = ClassB1B2
		m.catchUpPatch = deltaFC.Patch
		m.currentHunks = parseHunks(deltaFC.Patch)
		m.currentMarks = m.brain.HunkMarks(m.selectedPR.Repo, m.selectedPR.Number, m.selectedFile)
		m.hunkIdx = firstUnmarked(m.currentHunks, m.currentMarks)
		m.statusMsg = fmt.Sprintf("catch-up [%s]: f1→f2 since %s  (d: full diff)", ClassB1B2, shortSHA(m.catchUpOldHead))
		m.redrawDiff()
		m.jumpToCurrentHunk()
		return m, nil

	case diamondClassifiedMsg:
		if msg.err != nil {
			m.statusMsg = "classify: " + msg.err.Error()
			return m, nil
		}
		if m.view != viewDiff || m.selectedFile != msg.path {
			return m, nil
		}
		m.catchUpClass = msg.class

		if msg.class.Hidden() {
			// Nothing to show — auto-catch-up.
			m.catchUpMode = false
			label := msg.class.String()
			if msg.class.IsForget() {
				label = "FORGET — base absorbed feature"
			}
			m.statusMsg = fmt.Sprintf("✓ %s: %s (auto-caught-up)", m.selectedFile, label)
			m.brain.SetFileReviewed(m.selectedPR.Repo, m.selectedPR.Number, m.selectedFile, m.selectedPR.HeadSHA, m.selectedPR.BaseSHA)
			m.advanceCatchUpSession()
			return m, nil
		}

		// Shown class — display the catch-up diff.
		m.catchUpMode = true
		if msg.patch != "" {
			m.catchUpPatch = msg.patch
			m.currentHunks = parseHunks(msg.patch)
		}
		m.currentMarks = m.brain.HunkMarks(m.selectedPR.Repo, m.selectedPR.Number, m.selectedFile)
		m.hunkIdx = firstUnmarked(m.currentHunks, m.currentMarks)
		views := msg.class.Views()
		viewLabel := ""
		if len(views) > 0 {
			viewLabel = fmt.Sprintf("%s→%s", views[0].From, views[0].To)
		}
		m.statusMsg = fmt.Sprintf("catch-up [%s]: %s  (d: full diff)", msg.class, viewLabel)
		m.redrawDiff()
		m.jumpToCurrentHunk()
		return m, nil

	case blobLoadedMsg:
		if msg.err != nil {
			m.statusMsg = "blob: " + msg.err.Error()
			return m, nil
		}
		if m.view == viewDiff {
			m.blobContent = msg.content
			// Don't redraw if we're in catch-up mode — keep showing the delta.
			if !m.catchUpMode {
				m.redrawDiff()
				m.jumpToCurrentHunk()
			}
		}
		return m, nil

	case prefetchDoneMsg:
		// Prune cached PRs that are no longer open (merged/closed since
		// last session). freshKeys was populated by prsLoadedMsg handlers.
		if len(m.freshKeys) > 0 {
			var live []PR
			for _, p := range m.allPRs {
				if m.freshKeys[prKey(p.Repo, p.Number)] {
					live = append(live, p)
				}
			}
			m.allPRs = live
			m.rebuildPRItems()
			go m.brain.SetPRCache(m.allPRs)
		}
		m.prs.Title = fmt.Sprintf("PRs (%d)", len(m.allPRs))
		return m, nil

	case tea.KeyMsg:
		if m.view == viewDiff && m.noting {
			switch msg.String() {
			case "esc":
				m.noting = false
				m.noteInput.Blur()
				m.restoreDiffSize()
				return m, nil
			case "ctrl+d":
				body := strings.TrimSpace(m.noteInput.Value())
				m.noting = false
				m.noteInput.Blur()
				m.restoreDiffSize()
				if body != "" {
					if err := m.brain.SaveNote(m.selectedPR.Repo, m.selectedPR.Number, m.selectedFile, m.noteLineNo, m.noteLineHash, body); err != nil {
						m.statusMsg = "save note: " + err.Error()
					} else {
						m.currentNotes = m.brain.NotesForFile(m.selectedPR.Repo, m.selectedPR.Number, m.selectedFile)
						m.redrawDiff()
					}
				}
				return m, nil
			}
			var cmd tea.Cmd
			m.noteInput, cmd = m.noteInput.Update(msg)
			return m, cmd
		}
		if m.view == viewDiff {
			switch msg.String() {
			case "ctrl+c", "q":
				return m, tea.Quit
			case "esc":
				m.view = viewFiles
				m.rebuildFileItems()
				m.rebuildPRItems()
				return m, nil
			case "n", "down", "tab":
				if len(m.currentHunks) > 0 && m.hunkIdx < len(m.currentHunks)-1 {
					m.hunkIdx++
					m.redrawDiff()
					m.jumpToCurrentHunk()
				}
				return m, nil
			case "p", "up", "shift+tab":
				if m.hunkIdx > 0 {
					m.hunkIdx--
					m.redrawDiff()
					m.jumpToCurrentHunk()
				}
				return m, nil
			case " ", "x":
				if m.hunkIdx >= 0 && m.hunkIdx < len(m.currentHunks) {
					h := m.currentHunks[m.hunkIdx]
					if m.currentMarks == nil {
						m.currentMarks = map[string]bool{}
					}
					if m.currentMarks[h.Hash] {
						delete(m.currentMarks, h.Hash)
					} else {
						m.currentMarks[h.Hash] = true
					}
					m.saveMarks()
					if m.hunkIdx < len(m.currentHunks)-1 {
						m.hunkIdx++
					}
					m.redrawDiff()
					m.jumpToCurrentHunk()
				}
				return m, nil
			case "h", "left":
				m.view = viewFiles
				m.rebuildFileItems()
				m.rebuildPRItems()
				return m, nil
			case "m":
				// Mark every current hunk as seen.
				if m.currentMarks == nil {
					m.currentMarks = map[string]bool{}
				}
				for _, h := range m.currentHunks {
					m.currentMarks[h.Hash] = true
				}
				m.saveMarks()
				m.redrawDiff()
				m.jumpToCurrentHunk()
				return m, nil
			case "enter", "right":
				if m.allHunksMarked() {
					m.view = viewFiles
					m.rebuildFileItems()
					m.rebuildPRItems()
				}
				return m, nil
			case "j":
				m.moveCursor(1)
				return m, nil
			case "k":
				m.moveCursor(-1)
				return m, nil
			case "u":
				m.currentMarks = map[string]bool{}
				m.saveMarks()
				m.redrawDiff()
				m.hunkIdx = 0
				m.jumpToCurrentHunk()
				m.statusMsg = "cleared marks on " + m.selectedFile
				return m, nil
			case "d":
				// Toggle between catch-up diff and full PR diff.
				if m.catchUpOldHead == "" {
					// No catch-up available — nothing to toggle.
					return m, nil
				}
				fc, ok := m.currentFile()
				if !ok {
					return m, nil
				}
				if m.catchUpMode {
					// Switch to full diff.
					m.catchUpMode = false
					m.currentHunks = parseHunks(fc.Patch)
					m.currentMarks = m.brain.HunkMarks(m.selectedPR.Repo, m.selectedPR.Number, fc.Path)
					m.hunkIdx = firstUnmarked(m.currentHunks, m.currentMarks)
					m.statusMsg = "full diff  (d: catch-up diff)"
				} else {
					// Switch back to catch-up diff.
					m.catchUpMode = true
					m.currentHunks = parseHunks(m.catchUpPatch)
					m.currentMarks = m.brain.HunkMarks(m.selectedPR.Repo, m.selectedPR.Number, fc.Path)
					m.hunkIdx = firstUnmarked(m.currentHunks, m.currentMarks)
					m.statusMsg = fmt.Sprintf("catch-up [%s]: changes since %s  (d: full diff)", m.catchUpClass, shortSHA(m.catchUpOldHead))
				}
				m.redrawDiff()
				m.jumpToCurrentHunk()
				return m, nil
			case "c":
				lineNo := m.cursorFileLine()
				if lineNo == 0 {
					m.statusMsg = "cursor not on a file line"
					return m, nil
				}
				m.noting = true
				m.noteLineNo = lineNo
				m.noteLineHash = m.cursorLineHash(lineNo)
				m.noteInput.Reset()
				return m, m.noteInput.Focus()
			}
			// Fall through to let the viewport handle scrolling keys.
			var cmd tea.Cmd
			m.diff, cmd = m.diff.Update(msg)
			return m, cmd
		}

		// Tab switching in files view.
		if m.view == viewFiles && !listIsFiltering(m) {
			switch msg.String() {
			case "1":
				m.fileTab = tabFiles
				return m, nil
			case "2":
				m.fileTab = tabDescription
				m.rebuildInfoVP()
				return m, nil
			case "3":
				m.fileTab = tabNotes
				m.rebuildInfoVP()
				return m, nil
			}
		}

		// 's' toggles scrutiny on the selected PR.
		if m.view == viewPRs && !listIsFiltering(m) && msg.String() == "s" {
			if it, ok := m.prs.SelectedItem().(prItem); ok {
				on := !it.scrutinized
				m.brain.SetScrutiny(it.pr.Repo, it.pr.Number, on)
				m.rebuildPRItems()
				if on {
					m.statusMsg = fmt.Sprintf("scrutiny ON for %s#%d — full diffs, no catch-up shortcuts", it.pr.Repo, it.pr.Number)
				} else {
					m.statusMsg = fmt.Sprintf("scrutiny OFF for %s#%d", it.pr.Repo, it.pr.Number)
				}
			}
			return m, nil
		}

		// Non-diff views. vim-style h/l drill out/in as aliases for esc/enter.
		// Skip h/l while a filter is active so they're still typeable.
		switch msg.String() {
		case "ctrl+c", "q":
			if !listIsFiltering(m) {
				return m, tea.Quit
			}
		case "esc", "h", "left":
			if msg.String() == "h" && listIsFiltering(m) {
				break
			}
			if m.view == viewFiles {
				m.fileTab = tabFiles
				m.view = viewPRs
				return m, nil
			}
		case "enter", "l", "right":
			if msg.String() == "l" && listIsFiltering(m) {
				break
			}
			switch m.view {
			case viewPRs:
				if it, ok := m.prs.SelectedItem().(prItem); ok {
					pr := it.pr
					m.selectedPR = &pr
					m.catchUpSession = m.brain.ActiveCatchUp(pr.Repo, pr.Number)
					m.view = viewFiles
					key := prKey(pr.Repo, pr.Number)
					if _, cached := m.prFiles[key]; cached {
						m.rebuildFileItems()
						m.files.Title = fmt.Sprintf("Files in %s#%d", pr.Repo, pr.Number)
						return m, nil
					}
					m.loadingFiles = true
					m.files.Title = fmt.Sprintf("Files in %s#%d (loading...)", pr.Repo, pr.Number)
					m.files.SetItems(nil)
					return m, loadFilesCmd(pr)
				}
			case viewFiles:
				if m.fileTab != tabFiles {
					break
				}
				if it, ok := m.files.SelectedItem().(fileItem); ok {
					cmd := m.openFile(it.fc)
					return m, cmd
				}
			}
		}
	}

	var cmd tea.Cmd
	switch m.view {
	case viewPRs:
		prev := m.prs.Index()
		m.prs, cmd = m.prs.Update(msg)
		skipSectionHeaders(&m.prs, prev)
	case viewFiles:
		if m.fileTab != tabFiles {
			m.infoVP, cmd = m.infoVP.Update(msg)
		} else {
			prev := m.files.Index()
			m.files, cmd = m.files.Update(msg)
			skipSectionHeaders(&m.files, prev)
		}
	case viewDiff:
		m.diff, cmd = m.diff.Update(msg)
	}
	return m, cmd
}

// skipSectionHeaders nudges the cursor past non-interactive sectionItem
// headers. Direction is inferred from whether the index went up or down.
func skipSectionHeaders(l *list.Model, prevIdx int) {
	items := l.Items()
	cur := l.Index()
	if cur >= len(items) {
		return
	}
	if _, ok := items[cur].(sectionItem); !ok {
		return
	}
	dir := 1
	if cur < prevIdx {
		dir = -1
	}
	next := cur + dir
	if next >= 0 && next < len(items) {
		l.Select(next)
	}
}

func listIsFiltering(m model) bool {
	switch m.view {
	case viewPRs:
		return m.prs.FilterState() == list.Filtering
	case viewFiles:
		return m.files.FilterState() == list.Filtering
	}
	return false
}

var appStyle = lipgloss.NewStyle().Padding(0, 1)

func (m model) View() string {
	var body string
	switch m.view {
	case viewPRs:
		body = m.prs.View()
	case viewFiles:
		body = m.tabBar()
		switch m.fileTab {
		case tabFiles:
			body += m.files.View()
		case tabDescription, tabNotes:
			body += m.infoVP.View()
		}
	case viewDiff:
		if m.noting {
			body = m.renderNotingView()
		} else {
			body = m.diff.View()
		}
	}
	footer := m.statusMsg
	if footer == "" {
		switch m.view {
		case viewDiff:
			if m.noting {
				footer = fmt.Sprintf("line %d  ctrl+d: save  esc: cancel", m.noteLineNo)
			} else {
				marked := 0
				for _, h := range m.currentHunks {
					if m.currentMarks[h.Hash] {
						marked++
					}
				}
				total := len(m.currentHunks)
				cur := m.hunkIdx + 1
				if total == 0 {
					cur = 0
				}
				modeHint := ""
				if m.catchUpOldHead != "" {
					if m.catchUpMode {
						modeHint = fmt.Sprintf("  [catch-up %s since %s]  d: full diff", m.catchUpClass, shortSHA(m.catchUpOldHead))
					} else {
						modeHint = "  [full diff]  d: catch-up"
					}
				}
				footer = fmt.Sprintf("hunk %d/%d  marked %d/%d%s  ↑/↓: nav  j/k: cursor  space: toggle+next  m: mark all  c: note  u: unmark  h: back", cur, total, marked, total, modeHint)
			}
		case viewFiles:
			footer = "1: files  2: description  3: notes  l/enter: open  h/esc: back  q: quit"
		default:
			footer = "l/enter: open  s: scrutiny  h/esc: back  q: quit"
		}
	}
	return appStyle.Render(body) + "\n" + lipgloss.NewStyle().Faint(true).Render(footer)
}
