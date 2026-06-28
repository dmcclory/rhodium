package rhodium

import (
	"rhodium/internal/brain"
	"rhodium/internal/gh"
	coreglog "rhodium/internal/glog"

	tea "github.com/charmbracelet/bubbletea"
)

// enterGlog points the glog view at the selected PR. If the commit data is
// already cached it computes the rollup from current marks and shows it
// immediately; otherwise it shows an empty (loading) view and kicks off the
// fetch. Called from openPR (when the configured default lens is "commits")
// and from the NavigatedMsg handler (the `g` toggle from the files view).
func (a *app) enterGlog() tea.Cmd {
	pr := a.session.selectedPR
	if pr == nil {
		return nil
	}
	a.glog.BackRoute = a.session.listOrigin
	key := brain.PRKey(pr.Repo, pr.Number)
	if cd, ok := a.cache.prCommits[key]; ok {
		a.glog.SetCommits(pr, a.computeRollups(pr, cd.commits, cd.files))
		return nil
	}
	a.glog.SetCommits(pr, nil) // empty until the fetch lands
	return loadCommitsCmd(*pr)
}

// loadCommitsCmd fetches a PR's commits and, for each, the files it
// introduced. One ListPRCommits call plus one FetchCommitFiles per commit —
// review-scale PRs are small, and commit SHAs are immutable so the result is
// cached for the session.
func loadCommitsCmd(pr gh.PR) tea.Cmd {
	return func() tea.Msg {
		commits, err := gh.ListPRCommits(pr.Repo, pr.Number)
		if err != nil {
			return commitsLoadedMsg{pr: pr, err: err}
		}
		files := make(map[string][]gh.FileChange, len(commits))
		for _, c := range commits {
			fcs, err := gh.FetchCommitFiles(pr.Repo, c.SHA)
			if err != nil {
				return commitsLoadedMsg{pr: pr, err: err}
			}
			files[c.SHA] = fcs
		}
		return commitsLoadedMsg{pr: pr, commits: commits, files: files}
	}
}

func (a *app) onCommitsLoaded(msg commitsLoadedMsg) tea.Cmd {
	if msg.err != nil {
		a.status.msg = "error: " + msg.err.Error()
		return nil
	}
	key := brain.PRKey(msg.pr.Repo, msg.pr.Number)
	a.cache.prCommits[key] = commitData{commits: msg.commits, files: msg.files}
	// Only refresh the view if we're still on this PR.
	if a.session.selectedPR != nil && brain.PRKey(a.session.selectedPR.Repo, a.session.selectedPR.Number) == key {
		pr := msg.pr
		a.glog.SetCommits(&pr, a.computeRollups(&pr, msg.commits, msg.files))
	}
	return nil
}

// computeRollups gathers the marks for every path the commits touch and runs
// the Tier-1 hash-intersection rollup. Marks are read fresh so badges reflect
// the latest review state each time glog is entered.
func (a *app) computeRollups(pr *gh.PR, commits []gh.Commit, files map[string][]gh.FileChange) []coreglog.CommitRollup {
	marksByPath := map[string]map[string]int{}
	for _, fcs := range files {
		for _, f := range fcs {
			if _, done := marksByPath[f.Path]; done {
				continue
			}
			marksByPath[f.Path] = a.brain.HunkMarks(pr.Repo, pr.Number, f.Path)
		}
	}
	return coreglog.Rollup(commits, files, marksByPath)
}
