// Package keys carries the key-binding vocabulary shared by every TUI view.
// Binding describes one key→action record; Dispatch routes a pressed key to
// the first matching action across an ordered list of binding tables. View
// packages depend on this leaf package for their binding shape; the *app-
// flavored builders (globalBindings, agentBindings) live with the app.
package keys

import tea "github.com/charmbracelet/bubbletea"

// Binding is a single key → action record. One shape drives Dispatch, help
// overlay rendering, and (eventually) user-level key remapping. Keep it
// data-oriented: adding a feature should mean appending a Binding, not
// threading a new switch case through multiple files.
type Binding struct {
	Name       string         // stable id; future config can rebind by name
	Keys       []string       // all keys that trigger this binding (aliases)
	Desc       string         // shown in help overlay
	Group      string         // "Navigate" | "Mark" | "Notes" | "View" | "Agent" | "Global"
	Action     func() tea.Cmd // invoked on match; closures capture *app from their enclosing bindings() method
	Unfiltered bool           // if true, still fires while a bubbles list is in filter mode
}

// GroupOrder names the rendering order of binding groups in the help overlay.
var GroupOrder = []string{"Navigate", "Mark", "Notes", "Mention", "View", "Agent", "Global"}

// Dispatch walks the given binding tables in order and fires the first
// match. Returns (cmd, true) when a key matched so callers know whether to
// fall through to bubbles' own default handling.
//
// Filter gating is per-key, not per-binding: during list filter mode,
// single-character keys (letters, digits, punctuation) are skipped so they
// type into the filter instead of triggering actions. Multi-char keys
// ("esc", "enter", "tab", "ctrl+c", arrows) always fire — they can't be
// typed into a filter anyway. `Unfiltered: true` is the explicit escape
// hatch for single-char keys that must still work during filter (e.g. `?`).
func Dispatch(key string, filtering bool, tables ...[]Binding) (tea.Cmd, bool) {
	for _, tbl := range tables {
		for _, b := range tbl {
			for _, k := range b.Keys {
				if k != key {
					continue
				}
				if filtering && !b.Unfiltered && len(k) == 1 {
					continue
				}
				return b.Action(), true
			}
		}
	}
	return nil, false
}
