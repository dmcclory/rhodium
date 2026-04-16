package main

import (
	"fmt"
	"sync"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// program is a package-level handle to the running tea program, set in main
// before Run(). Goroutines spawned from tea.Cmd's use it to push messages
// back onto the update loop.
var program *tea.Program

type view int

const (
	viewPRs view = iota
	viewFiles
	viewDiff
)

// --- list items ---

type prItem struct {
	pr      PR
	summary string
}

func (i prItem) Title() string {
	head := fmt.Sprintf("%s#%d  %s  @%s", i.pr.Repo, i.pr.Number, i.pr.Title, i.pr.Author)
	if i.summary == "" {
		return head
	}
	return head + "  (" + i.summary + ")"
}
func (i prItem) Description() string { return "" }
func (i prItem) FilterValue() string { return i.Title() }

type fileItem struct {
	fc     FileChange
	status FileStatus
}

func (i fileItem) Title() string {
	return fmt.Sprintf("%s %s  +%d -%d", i.status.Glyph(), i.fc.Path, i.fc.Additions, i.fc.Deletions)
}
func (i fileItem) Description() string { return "" }
func (i fileItem) FilterValue() string { return i.fc.Path }

// sectionItem is a non-interactive header used to group list entries into
// "in progress" / "unseen" buckets. Enter/l handlers ignore it via type
// assertion.
type sectionItem struct{ label string }

var sectionHeaderStyle = lipgloss.NewStyle().Faint(true).Bold(true)

func (s sectionItem) Title() string       { return sectionHeaderStyle.Render(s.label) }
func (s sectionItem) Description() string { return "" }
func (s sectionItem) FilterValue() string { return "" }

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// --- messages ---

type prsLoadedMsg struct {
	prs []PR
	err error
}
type inProgressLoadedMsg struct {
	prs []PR
	err error
}
type filesLoadedMsg struct {
	pr    PR
	files []FileChange
	err   error
}
type prefetchDoneMsg struct{}

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

	allPRs       []PR
	prFiles      map[string][]FileChange // prKey → files
	selectedPR   *PR
	selectedFile string

	// Diff view state: which file is open, its parsed hunks, current mark set,
	// and the line-offset table for navigation.
	currentHunks []Hunk
	currentMarks map[string]bool
	hunkLines    []int
	hunkIdx      int

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
	prList.Title = "PRs (loading...)"

	fileList := list.New(nil, compactDelegate(), 0, 0)
	fileList.Title = "Files"

	vp := viewport.New(0, 0)

	return model{
		cfg:     cfg,
		brain:   brain,
		view:    viewPRs,
		prs:     prList,
		files:   fileList,
		diff:    vp,
		prFiles: map[string][]FileChange{},
	}
}

func (m model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.loadInProgressCmd()}
	for _, repo := range m.cfg.Repos {
		cmds = append(cmds, loadRepoPRsCmd(repo))
	}
	return tea.Batch(cmds...)
}

// loadInProgressCmd fetches metadata for every PR the brain knows about, in
// parallel, so in-progress PRs can render before the (slower) full repo
// listings arrive.
func (m model) loadInProgressCmd() tea.Cmd {
	refs := m.brain.InProgressRefs()
	return func() tea.Msg {
		if len(refs) == 0 {
			return inProgressLoadedMsg{}
		}
		results := make([]PR, len(refs))
		errs := make([]error, len(refs))
		var wg sync.WaitGroup
		for i, ref := range refs {
			wg.Add(1)
			go func(i int, ref PRRef) {
				defer wg.Done()
				pr, err := fetchPR(ref.Repo, ref.Number)
				results[i] = pr
				errs[i] = err
			}(i, ref)
		}
		wg.Wait()
		var prs []PR
		for i, pr := range results {
			if errs[i] != nil {
				continue // tolerate — may be closed/merged/renamed repo
			}
			prs = append(prs, pr)
		}
		return inProgressLoadedMsg{prs: prs}
	}
}

// loadRepoPRsCmd fetches PRs for a single repo and returns a prsLoadedMsg.
// Runs one per repo via tea.Batch so each repo renders independently — the
// in-progress bucket stays stable at the top; untouched PRs fill in below as
// they arrive.
func loadRepoPRsCmd(repo string) tea.Cmd {
	return func() tea.Msg {
		prs, err := listPRs(repo)
		if err != nil {
			return prsLoadedMsg{err: fmt.Errorf("%s: %w", repo, err)}
		}
		return prsLoadedMsg{prs: prs}
	}
}

func loadFilesCmd(pr PR) tea.Cmd {
	return func() tea.Msg {
		return fetchOne(pr)
	}
}

func fetchOne(pr PR) filesLoadedMsg {
	files, err := listPRFiles(pr.Repo, pr.Number)
	if err != nil {
		return filesLoadedMsg{pr: pr, err: err}
	}
	return filesLoadedMsg{pr: pr, files: files}
}

func prefetchAllCmd(prs []PR) tea.Cmd {
	const workers = 4
	return func() tea.Msg {
		jobs := make(chan PR)
		done := make(chan struct{})
		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for pr := range jobs {
					program.Send(fetchOne(pr))
				}
			}()
		}
		go func() {
			for _, pr := range prs {
				jobs <- pr
			}
			close(jobs)
			wg.Wait()
			close(done)
		}()
		<-done
		return prefetchDoneMsg{}
	}
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
		it := prItem{pr: pr}
		// A PR is "in progress" if the brain has any marks for it, even
		// before we've fetched its file list. This keeps already-touched
		// PRs from popping between buckets during startup prefetch.
		looked := m.brain.HasAnyMarks(pr.Repo, pr.Number)
		if files, ok := m.prFiles[prKey(pr.Repo, pr.Number)]; ok {
			unseen := m.brain.UnseenCount(pr.Repo, pr.Number, files)
			total := len(files)
			if unseen == 0 {
				it.summary = "✓ caught up"
			} else {
				it.summary = fmt.Sprintf("%d new", unseen)
			}
			if unseen < total {
				looked = true
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
	var inProgress, unseen []fileItem
	for _, fc := range files {
		status := m.brain.Status(m.selectedPR.Repo, m.selectedPR.Number, fc)
		fi := fileItem{fc: fc, status: status}
		if status == StatusUnseen {
			unseen = append(unseen, fi)
		} else {
			inProgress = append(inProgress, fi)
		}
	}
	var items []list.Item
	if len(inProgress) > 0 {
		items = append(items, sectionItem{label: "── in progress ──"})
		for _, fi := range inProgress {
			items = append(items, fi)
		}
	}
	if len(unseen) > 0 {
		if len(inProgress) > 0 {
			items = append(items, sectionItem{label: "── unseen ──"})
		}
		for _, fi := range unseen {
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

// openFile loads a file into the diff view: parse hunks, seed marks from the
// brain, render, and position on the first unmarked hunk.
func (m *model) openFile(fc FileChange) {
	m.selectedFile = fc.Path
	m.view = viewDiff
	m.currentHunks = parseHunks(fc.Patch)
	m.currentMarks = m.brain.HunkMarks(m.selectedPR.Repo, m.selectedPR.Number, fc.Path)
	m.redrawDiff()
	m.hunkIdx = firstUnmarked(m.currentHunks, m.currentMarks)
	m.jumpToCurrentHunk()
}

func firstUnmarked(hunks []Hunk, marks map[string]bool) int {
	for i, h := range hunks {
		if !marks[h.Hash] {
			return i
		}
	}
	return 0
}

func (m *model) redrawDiff() {
	if len(m.currentHunks) == 0 {
		m.diff.SetContent("(no hunks — nothing to review)")
		m.hunkLines = nil
		return
	}
	body, lines := renderHunks(m.currentHunks, m.currentMarks, m.hunkIdx)
	m.diff.SetContent(body)
	m.hunkLines = lines
}

func (m *model) jumpToCurrentHunk() {
	if m.hunkIdx < 0 || m.hunkIdx >= len(m.hunkLines) {
		return
	}
	m.diff.SetYOffset(m.hunkLines[m.hunkIdx])
}

func (m *model) saveMarks() {
	if m.selectedPR == nil || m.selectedFile == "" {
		return
	}
	if err := m.brain.SetHunkMarks(m.selectedPR.Repo, m.selectedPR.Number, m.selectedFile, m.currentMarks); err != nil {
		m.statusMsg = "save error: " + err.Error()
	}
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
		return m, nil

	case inProgressLoadedMsg:
		if msg.err != nil {
			m.statusMsg = "error: " + msg.err.Error()
			return m, nil
		}
		// Seed the list with in-progress PRs immediately. These go on top
		// (rebuildPRItems buckets them via HasAnyMarks) so the reviewer sees
		// their active work before any repo listing finishes.
		added := m.mergePRs(msg.prs)
		m.rebuildPRItems()
		if len(added) > 0 {
			return m, prefetchAllCmd(added)
		}
		return m, nil

	case prsLoadedMsg:
		if msg.err != nil {
			m.statusMsg = "error: " + msg.err.Error()
			return m, nil
		}
		added := m.mergePRs(msg.prs)
		m.rebuildPRItems()
		m.prs.Title = fmt.Sprintf("PRs (%d, loading files…)", len(m.allPRs))
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
		return m, nil

	case prefetchDoneMsg:
		m.prs.Title = fmt.Sprintf("PRs (%d)", len(m.allPRs))
		return m, nil

	case tea.KeyMsg:
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
					// Advance to the next hunk after toggling, so you can
					// space-space-space through a review.
					if m.hunkIdx < len(m.currentHunks)-1 {
						m.hunkIdx++
					}
					m.redrawDiff()
					m.jumpToCurrentHunk()
				}
				return m, nil
			case "h":
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
				m.statusMsg = "marked all hunks in " + m.selectedFile
				return m, nil
			case "u":
				// Unmark every current hunk.
				m.currentMarks = map[string]bool{}
				m.saveMarks()
				m.redrawDiff()
				m.hunkIdx = 0
				m.jumpToCurrentHunk()
				m.statusMsg = "cleared marks on " + m.selectedFile
				return m, nil
			}
			// Fall through to let the viewport handle scrolling keys.
			var cmd tea.Cmd
			m.diff, cmd = m.diff.Update(msg)
			return m, cmd
		}

		// Non-diff views. vim-style h/l drill out/in as aliases for esc/enter.
		// Skip h/l while a filter is active so they're still typeable.
		switch msg.String() {
		case "ctrl+c", "q":
			if !listIsFiltering(m) {
				return m, tea.Quit
			}
		case "esc", "h":
			if msg.String() == "h" && listIsFiltering(m) {
				break
			}
			if m.view == viewFiles {
				m.view = viewPRs
				return m, nil
			}
		case "enter", "l":
			if msg.String() == "l" && listIsFiltering(m) {
				break
			}
			switch m.view {
			case viewPRs:
				if it, ok := m.prs.SelectedItem().(prItem); ok {
					pr := it.pr
					m.selectedPR = &pr
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
				if it, ok := m.files.SelectedItem().(fileItem); ok {
					m.openFile(it.fc)
					return m, nil
				}
			}
		}
	}

	var cmd tea.Cmd
	switch m.view {
	case viewPRs:
		m.prs, cmd = m.prs.Update(msg)
	case viewFiles:
		m.files, cmd = m.files.Update(msg)
	case viewDiff:
		m.diff, cmd = m.diff.Update(msg)
	}
	return m, cmd
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
		body = m.files.View()
	case viewDiff:
		body = m.diff.View()
	}
	footer := m.statusMsg
	if footer == "" {
		switch m.view {
		case viewDiff:
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
			footer = fmt.Sprintf("hunk %d/%d  marked %d/%d  ↑/↓: nav  space: toggle+next  m: mark all  u: unmark  h: back", cur, total, marked, total)
		default:
			footer = "l/enter: open  h/esc: back  q: quit"
		}
	}
	return appStyle.Render(body) + "\n" + lipgloss.NewStyle().Faint(true).Render(footer)
}
