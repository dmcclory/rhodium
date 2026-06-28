package glog

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"rhodium/internal/gh"
	coreglog "rhodium/internal/glog"
)

var (
	railStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	markedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("10")) // green  [✓]
	partialStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // yellow [~]
	shaStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	authorStyle  = lipgloss.NewStyle().Faint(true)
	focusedStyle = lipgloss.NewStyle().Reverse(true).Bold(true)
	summaryStyle = lipgloss.NewStyle().Faint(true)
	headerStyle  = lipgloss.NewStyle().Bold(true)
)

// badge renders the per-commit mark rollup glyph.
func badge(s coreglog.Status) string {
	switch s {
	case coreglog.StatusAll:
		return markedStyle.Render("[✓]")
	case coreglog.StatusPartial:
		return partialStyle.Render("[~]")
	default:
		return "[ ]"
	}
}

// statusTail is the right-hand summary for a commit row.
func statusTail(c coreglog.CommitRollup) string {
	switch c.Status {
	case coreglog.StatusAll:
		return "✔ reviewed"
	case coreglog.StatusPartial:
		return fmt.Sprintf("◐ %d/%d hunks", c.Marked, c.Total)
	default:
		if c.Total == 0 {
			return "" // no markable hunks (e.g. merge commit)
		}
		return "○ unreviewed"
	}
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// commitRow renders a single collapsed commit line (without the rail/cursor
// chrome, which renderCommits adds).
func commitRow(c coreglog.CommitRollup) string {
	parts := []string{badge(c.Status), shaStyle.Render(shortSHA(c.Commit.SHA)), c.Commit.Title}
	if c.Commit.Author != "" {
		parts = append(parts, authorStyle.Render(c.Commit.Author))
	}
	if tail := statusTail(c); tail != "" {
		parts = append(parts, tail)
	}
	return strings.Join(parts, "  ")
}

// renderCommits produces the collapsed glog body: a header, one node per
// commit connected by a │ rail, and a progress summary. cursor is the index
// of the focused commit (rendered reverse-video).
func renderCommits(pr *gh.PR, commits []coreglog.CommitRollup, cursor int) string {
	var b strings.Builder

	if pr != nil {
		b.WriteString(headerStyle.Render(fmt.Sprintf(" %s#%d · %q · %d commits", pr.Repo, pr.Number, pr.Title, len(commits))) + "\n\n")
	}

	reviewed, marked, total := 0, 0, 0
	for i, c := range commits {
		if c.Status == coreglog.StatusAll {
			reviewed++
		}
		marked += c.Marked
		total += c.Total

		row := commitRow(c)
		if i == cursor {
			row = focusedStyle.Render(row)
		}
		b.WriteString("  " + railStyle.Render("●") + "  " + row + "\n")
		if i < len(commits)-1 {
			b.WriteString("  " + railStyle.Render("│") + "\n")
		}
	}

	b.WriteString("\n" + summaryStyle.Render(fmt.Sprintf("reviewed %d/%d commits · %d/%d hunks", reviewed, len(commits), marked, total)) + "\n")
	return b.String()
}
