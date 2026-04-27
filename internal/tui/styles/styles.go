// Package styles holds lipgloss styles shared across TUI views. Per-view
// private styles stay with their view; this package is for the handful of
// styles referenced from more than one place.
package styles

import "github.com/charmbracelet/lipgloss"

// App is the outer frame applied to every view's body.
var App = lipgloss.NewStyle().Padding(0, 1)

// Status* styles paint PR review-state badges (PRs view) and review-event
// labels (comments view).
var (
	StatusApproved = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	StatusChanges  = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	StatusReview   = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
)

// Help* styles paint the help overlay box and title.
var (
	HelpBox = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(1, 2)

	HelpTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("63")).
			MarginBottom(1)
)
