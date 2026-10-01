package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattermost/mattermost/server/public/model"
)

type paneKind int

const (
	paneChannel paneKind = iota
	paneThread
)

// A thread opens as a right-hand panel when the message area is at least
// this wide; narrower screens show it in place of the channel.
const splitMinW = 110

// pane is one message column with its own scroll position, selection and
// hit-testing data. The channel and the open thread each have one.
type pane struct {
	vp        viewport.Model
	spans     []span
	links     map[int]string // content line -> URL opened on click
	imgs      []imgPlace
	visible   []*model.Post
	content   []string // rendered lines, for copying selections
	sel       int
	renderedW int
}

func newPane() *pane {
	vp := viewport.New(0, 0)
	vp.MouseWheelEnabled = false
	vp.KeyMap = viewport.KeyMap{}
	return &pane{vp: vp, links: map[int]string{}}
}

func newInput() textarea.Model {
	ta := textarea.New()
	ta.Prompt = ""
	ta.ShowLineNumbers = false
	ta.CharLimit = 16383
	ta.MaxHeight = 0
	ta.SetHeight(1)
	ta.FocusedStyle.CursorLine = ta.FocusedStyle.Base
	ta.BlurredStyle.CursorLine = ta.BlurredStyle.Base
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter", "ctrl+j"))
	ta.KeyMap.LineNext = key.NewBinding(key.WithKeys("down"))
	ta.KeyMap.LinePrevious = key.NewBinding(key.WithKeys("up"))
	return ta
}

// p is the pane that receives keys and whose draft is in m.input.
func (m *Model) p() *pane { return m.panes[m.active] }

// inputFor returns the draft of a pane: the active one lives in m.input,
// the other in m.draft.
func (m *Model) inputFor(k paneKind) *textarea.Model {
	if k == m.active {
		return &m.input
	}
	return &m.draft
}

func (m *Model) setActive(k paneKind) tea.Cmd {
	if m.active == k {
		return nil
	}
	m.input, m.draft = m.draft, m.input
	m.active = k
	m.draft.Blur()
	m.clearSugs()
	return m.input.Focus()
}

// ---- geometry ----

type paneGeo struct {
	x, w     int // relative to the start of the message area; w == 0 hides the pane
	vpTop    int
	vpH      int
	sugH     int
	attH     int
	inputH   int
	inputTop int
}

type geo struct {
	sideW    int // sidebar width including its right border, 0 when hidden
	sideTop  int
	sideRows int
	mainW    int
	split    bool
	panes    [2]paneGeo
}

func inputLines(ta *textarea.Model, innerW int) int {
	n := 0
	for _, line := range strings.Split(ta.Value(), "\n") {
		n += max(1, (ansi.StringWidth(line)+innerW)/innerW)
	}
	return min(max(n, 1), maxInputLines)
}

func (m *Model) geo() geo {
	var g geo
	if m.w >= 70 {
		g.sideW = sidebarWidth + 1
	}
	g.sideTop = 2
	g.sideRows = max(m.h-1-g.sideTop, 1)
	g.mainW = max(m.w-g.sideW, 20)
	g.split = m.thread != nil && g.mainW >= splitMinW

	switch {
	case g.split:
		tw := min(max(g.mainW*42/100, 44), 72)
		cw := g.mainW - tw - 1 // one column for the divider
		g.panes[paneChannel] = paneGeo{x: 0, w: cw}
		g.panes[paneThread] = paneGeo{x: cw + 1, w: tw}
	case m.thread != nil:
		g.panes[paneThread] = paneGeo{w: g.mainW}
	default:
		g.panes[paneChannel] = paneGeo{w: g.mainW}
	}
	for k := range g.panes {
		pg := &g.panes[k]
		if pg.w == 0 {
			continue
		}
		pg.vpTop = 2
		pg.inputH = inputLines(m.inputFor(paneKind(k)), max(pg.w-4, 1))
		if paneKind(k) == m.active {
			pg.sugH = min(len(m.sugs), maxSugs)
			if len(m.attach) > 0 {
				pg.attH = 1
			}
		}
		// footer 1, input border 2, status 1
		pg.inputTop = m.h - 1 - (pg.inputH + 2)
		pg.vpH = max(pg.inputTop-1-pg.attH-pg.sugH-pg.vpTop, 1)
	}
	return g
}

// paneAt returns the visible pane under screen column x.
func (g geo) paneAt(x int) paneKind {
	rel := x - g.sideW
	if g.split && rel >= g.panes[paneThread].x {
		return paneThread
	}
	if g.panes[paneChannel].w == 0 {
		return paneThread
	}
	return paneChannel
}

func (m *Model) layout() {
	// the thread pane owns the keyboard whenever it replaces the channel
	switch {
	case m.thread == nil && m.active == paneThread:
		m.setActive(paneChannel)
	case m.thread != nil && m.active == paneChannel && m.w-sidebarW(m.w) < splitMinW:
		m.setActive(paneThread)
	}
	g := m.geo()
	for k, pg := range g.panes {
		if pg.w == 0 {
			continue
		}
		ta := m.inputFor(paneKind(k))
		ta.SetWidth(max(pg.w-4, 1))
		ta.SetHeight(pg.inputH)
		p := m.panes[k]
		atBottom := p.vp.AtBottom()
		p.vp.Width = pg.w
		p.vp.Height = pg.vpH
		if p.renderedW != p.vp.Width {
			// width changed (resize, thread opened/closed): re-wrap
			m.refreshPane(paneKind(k), atBottom)
		} else if atBottom {
			p.vp.GotoBottom()
		}
	}
	// follow the cursor only after it moved, so wheel scrolling sticks
	if m.sideFollow {
		m.ensureSideVisible(g.sideRows)
		m.sideFollow = false
	}
	m.sideOffset = min(m.sideOffset, max(len(m.rows)-g.sideRows, 0))
}

func sidebarW(w int) int {
	if w >= 70 {
		return sidebarWidth + 1
	}
	return 0
}

// padTop is the number of blank rows above content shorter than the viewport.
func (m *Model) padTop(k paneKind) int {
	p := m.panes[k]
	return max(p.vp.Height-p.vp.TotalLineCount(), 0)
}

// refresh re-renders both panes. The active one sticks to the bottom when
// forced; each sticks when it was already at the bottom.
func (m *Model) refresh(forceBottom bool) {
	m.refreshPane(m.active, forceBottom)
	other := paneChannel
	if m.active == paneChannel {
		other = paneThread
	}
	m.refreshPane(other, false)
	m.inputFor(paneThread).Placeholder = tr("Reply to thread…")
	if it := m.items[m.cur]; it != nil {
		m.inputFor(paneChannel).Placeholder = tr("Message ") + symbol(it.ch) + it.label + keys(tr("   (/ commands, @ mentions, Ctrl+K switch)"))
	}
}

func (m *Model) refreshPane(k paneKind, forceBottom bool) {
	p := m.panes[k]
	if m.cur == "" || (k == paneThread && m.thread == nil) {
		p.vp.SetContent("")
		p.spans, p.visible, p.imgs, p.content = nil, nil, nil, nil
		p.links = map[int]string{}
		return
	}
	atBottom := p.vp.AtBottom() || p.vp.TotalLineCount() == 0
	content := m.renderPosts(k, max(p.vp.Width-1, 10))
	p.vp.SetContent(content)
	p.content = strings.Split(content, "\n")
	p.renderedW = p.vp.Width
	if forceBottom || atBottom {
		p.vp.GotoBottom()
	}
}

// ---- drawing ----

func (m *Model) viewMain(g geo) string {
	if m.sw != nil || m.showHelp || m.wiz != nil || m.result != nil || m.shell != nil {
		var box string
		switch {
		case m.shell != nil:
			box = m.viewShell(g.mainW, m.h)
		case m.result != nil:
			box = m.viewResult(g.mainW)
		case m.wiz != nil:
			box = m.viewWizard(g.mainW)
		case m.sw != nil:
			box = m.viewSwitcher(g.mainW)
		default:
			box = helpBox()
		}
		return lipgloss.Place(g.mainW, m.h-1, lipgloss.Center, lipgloss.Top, "\n\n"+box)
	}
	var cols []string
	for k, pg := range g.panes {
		if pg.w == 0 {
			continue
		}
		if len(cols) > 0 {
			div := lipgloss.NewStyle().Foreground(colBorder).Render(strings.TrimSuffix(strings.Repeat("│\n", m.h-1), "\n"))
			cols = append(cols, div)
		}
		cols = append(cols, m.viewPane(paneKind(k), pg, g.split))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cols...)
}

func (m *Model) paneTitle(k paneKind, split bool) string {
	if k == paneThread {
		lbl := ""
		if it := m.items[m.thread.channel]; it != nil {
			lbl = symbol(it.ch) + it.label
		}
		hint := tr(" · Esc back")
		if split {
			hint = tr(" · Esc close")
		}
		return stTitle.Render(" Thread") + stDim.Render(tr(" in ")+lbl+hint)
	}
	it := m.items[m.cur]
	if it == nil {
		return ""
	}
	title := stTitle.Render(" " + symbol(it.ch) + it.label)
	if m.activeCall(m.cur) {
		title += stAccent.Bold(true).Render(tr("  ● call in progress · /call to join"))
	}
	if hdr := strings.TrimSpace(strings.SplitN(it.ch.Header, "\n", 2)[0]); hdr != "" {
		title += stDim.Render("  " + hdr)
	}
	return title
}

func (m *Model) viewPane(k paneKind, pg paneGeo, split bool) string {
	w := pg.w
	p := m.panes[k]
	active := k == m.active
	ruleColor := colBorder
	if split && active {
		ruleColor = colAccent
	}
	title := ansi.Truncate(m.paneTitle(k, split), w, "…")
	rule := lipgloss.NewStyle().Foreground(ruleColor).Render(strings.Repeat("─", w))

	body := m.highlight(k, p.vp.View(), w)
	if pad := m.padTop(k); pad > 0 {
		// short conversations sit on the input box, like other chat apps
		lines := strings.Split(body, "\n")
		body = strings.Repeat("\n", pad) + strings.Join(lines[:len(lines)-pad], "\n")
	}
	if pg.sugH > 0 {
		body += "\n" + m.viewSugs(w, pg.sugH)
	}
	bodyH := pg.vpH + pg.sugH
	body = lipgloss.NewStyle().Height(bodyH).MaxHeight(bodyH).Render(body)

	box := stInputBox
	switch {
	case active && m.focus == focusInput && strings.HasPrefix(m.input.Value(), "!"):
		box = stInputShell // a local command, not a message
	case active && m.focus == focusInput:
		box = stInputOn
	}
	input := box.Width(w - 2).Render(m.inputFor(k).View())

	parts := []string{title, rule, body, m.viewStatus(k, w)}
	if pg.attH > 0 {
		parts = append(parts, ansi.Truncate(m.viewAttachments(w), w, "…"))
	}
	return lipgloss.NewStyle().Width(w).Render(lipgloss.JoinVertical(lipgloss.Left, append(parts, input)...))
}

func (m *Model) viewStatus(k paneKind, w int) string {
	key := m.cur + "|"
	if k == paneThread && m.thread != nil {
		key += m.thread.root
	}
	var s string
	if users := m.typing[key]; len(users) > 0 {
		var names []string
		for id := range users {
			names = append(names, m.c.Username(id))
		}
		s = stDim.Italic(true).Render(" " + strings.Join(names, ", ") + tr(" typing…"))
	} else if m.status != "" && k == m.active {
		if m.statusErr {
			s = stErr.Render(" " + m.status)
		} else {
			s = stDim.Render(" " + m.status)
		}
	} else if !m.panes[k].vp.AtBottom() {
		s = stAccent.Render(tr(" ↓ more below · PgDn"))
	}
	return ansi.Truncate(s, w, "…")
}
