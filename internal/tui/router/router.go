// Package router carries the cross-view navigation vocabulary. View
// packages emit router.Navigate(routeX) instead of importing each other,
// so views know route names but not other views' types.
package router

import tea "github.com/charmbracelet/bubbletea"

// Route names a destination view. Compared as a typed string for
// debuggability; the constants below enumerate the legal values.
type Route string

const (
	RouteTodo     Route = "todo"
	RoutePRs      Route = "prs"
	RouteFiles    Route = "files"
	RouteDiff     Route = "diff"
	RouteComments Route = "comments"
)

// NavigatedMsg is what app.Update receives to flip the active view.
type NavigatedMsg struct{ To Route }

// Navigate is the tea.Cmd a binding returns to request navigation.
func Navigate(to Route) tea.Cmd {
	return func() tea.Msg { return NavigatedMsg{To: to} }
}
