package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The new-messages bar sits in the rule under the conversation's title and
// names every other conversation with something unread, most recent first,
// so a message to a channel scrolled out of the sidebar is not missed. It
// works with or without the Unreads filter. Clicking a name opens it.

// newBarName is how many cells a name gets: enough to tell who, not the
// full name.
const newBarName = 14

type newChip struct {
	id     string
	text   string // rendered
	x0, x1 int    // columns within the pane
}

// newBarHost reports whether pane k draws the bar: the channel pane, or the
// thread when it fills the screen.
func newBarHost(k paneKind, split bool) bool {
	return k == paneChannel || !split
}

// newBarChips lays out the bar for a pane w cells wide; more counts the
// conversations that did not fit.
func (m *Model) newBarChips(w int) (chips []newChip, more int) {
	var list []*chanItem
	for _, it := range m.items {
		if it.ch.Id != m.cur && (it.ch.TeamId == m.team || isDM(it.ch)) && m.hasUnread(it) {
			list = append(list, it)
		}
	}
	sortByRecent(list)
	x := 2 // after "─ "
	for i, it := range list {
		name := ansi.Truncate(symbol(it.ch)+it.label, newBarName, "…")
		text := stSideUnread.Render(name)
		if it.mentions > 0 {
			text = stMention.Render(name) + stBadge.Render(fmt.Sprintf(" %d ", it.mentions))
		}
		x0 := x
		if i > 0 {
			x0 += 2
		}
		x1 := x0 + ansi.StringWidth(text)
		tail := 2 // " ─"
		if left := len(list) - i - 1; left > 0 {
			tail += len(fmt.Sprintf(" +%d", left))
		}
		if x1+tail > w {
			return chips, len(list) - i
		}
		chips = append(chips, newChip{id: it.ch.Id, text: text, x0: x0, x1: x1})
		x = x1
	}
	return chips, 0
}

// newBar is the rule under a pane's title, with the bar drawn into it.
func (m *Model) newBar(w int, rule lipgloss.Style) string {
	chips, more := m.newBarChips(w)
	if len(chips) == 0 {
		if more > 0 {
			return rule.Render("─ ") + stMention.Render(fmt.Sprintf("+%d", more)) + rule.Render(" "+strings.Repeat("─", max(w-4-len(fmt.Sprint(more)), 0)))
		}
		return rule.Render(strings.Repeat("─", w))
	}
	var b strings.Builder
	b.WriteString(rule.Render("─ "))
	for i, c := range chips {
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(c.text)
	}
	x := chips[len(chips)-1].x1
	if more > 0 {
		s := fmt.Sprintf(" +%d", more)
		b.WriteString(stDim.Render(s))
		x += len(s)
	}
	b.WriteString(rule.Render(" " + strings.Repeat("─", max(w-x-1, 0))))
	return b.String()
}

// newBarAt returns the conversation under column x of the bar, if any.
func (m *Model) newBarAt(w, x int) string {
	chips, _ := m.newBarChips(w)
	for _, c := range chips {
		if x >= c.x0 && x < c.x1 {
			return c.id
		}
	}
	return ""
}
