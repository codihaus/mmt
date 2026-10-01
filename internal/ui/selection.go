package ui

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Mouse mode is on for clicks and scrolling, which stops the terminal from
// selecting text itself. Dragging over messages therefore selects in the app
// and copies on release, the way terminals do.

type cellPos struct{ line, col int }

type textSel struct {
	pane  paneKind
	from  cellPos // where the drag started
	to    cellPos
	moved bool
	// press position, replayed as a click when the mouse did not move
	pressX, pressY int
}

var stSelected = lipgloss.NewStyle().Reverse(true)

// ordered returns the selection start and end in reading order.
func (s *textSel) ordered() (a, b cellPos) {
	a, b = s.from, s.to
	if b.line < a.line || (b.line == a.line && b.col < a.col) {
		a, b = b, a
	}
	return a, b
}

// cols returns the selected column range [c0, c1) on a content line.
func (s *textSel) cols(line, width int) (int, int, bool) {
	a, b := s.ordered()
	if line < a.line || line > b.line {
		return 0, 0, false
	}
	c0, c1 := 0, width
	if line == a.line {
		c0 = a.col
	}
	if line == b.line {
		c1 = b.col + 1
	}
	return c0, c1, c1 > c0
}

// cellAt maps a screen position to a content cell of a pane, clamped to its
// message area.
func (m *Model) cellAt(k paneKind, x, y int) cellPos {
	g := m.geo()
	pg, pn := g.panes[k], m.panes[k]
	row := min(max(y-pg.vpTop, 0), pn.vp.Height-1)
	col := min(max(x-g.sideW-pg.x, 0), pg.w-1)
	return cellPos{line: pn.vp.YOffset + row - m.padTop(k), col: col}
}

// highlight inverts the selected cells of the visible viewport lines.
func (m *Model) highlight(k paneKind, view string, width int) string {
	s := m.textSel
	if s == nil || s.pane != k || !s.moved {
		return view
	}
	off := m.panes[k].vp.YOffset
	lines := strings.Split(view, "\n")
	for i, l := range lines {
		c0, c1, ok := s.cols(off+i, width)
		if !ok {
			continue
		}
		mid := ansi.Strip(ansi.Cut(l, c0, c1))
		if mid == "" {
			continue
		}
		lines[i] = ansi.Cut(l, 0, c0) + stSelected.Render(mid) + ansi.Cut(l, c1, width)
	}
	return strings.Join(lines, "\n")
}

// frameChars are the bubble borders, gutter and selection marker; they are
// left out of copied text.
const frameChars = "│╭╮╰╯─▌"

// selectedText returns the plain text under the selection with message
// frames removed.
func (m *Model) selectedText() string {
	s := m.textSel
	pn := m.panes[s.pane]
	a, b := s.ordered()
	var out []string
	for ln := a.line; ln <= b.line; ln++ {
		if ln < 0 || ln >= len(pn.content) {
			continue
		}
		c0, c1, ok := s.cols(ln, pn.vp.Width)
		if !ok {
			continue
		}
		out = append(out, cleanCopied(ansi.Cut(ansi.Strip(pn.content[ln]), c0, c1)))
	}
	// drop blank lines at the edges (frame tops and bottoms)
	for len(out) > 0 && out[0] == "" {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n")
}

func isFrame(r rune) bool { return strings.ContainsRune(frameChars, r) }

func startsWithFrame(s string) bool {
	for _, r := range s {
		return isFrame(r)
	}
	return false
}

// cleanCopied strips frame borders and the one space of padding next to
// them, but keeps indentation inside the message (code blocks).
func cleanCopied(s string) string {
	s = strings.TrimRightFunc(s, func(r rune) bool { return unicode.IsSpace(r) || isFrame(r) })
	rest := strings.TrimLeft(s, " ")
	for startsWithFrame(rest) {
		rest = strings.TrimLeftFunc(rest, isFrame)
		rest = strings.TrimPrefix(rest, " ")
		// the selection marker sits before the indent of right-aligned frames
		if t := strings.TrimLeft(rest, " "); startsWithFrame(t) {
			rest = t
		}
	}
	return rest
}
