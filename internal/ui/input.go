package ui

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// ---- keyboard ----

func (m *Model) handleKey(k tea.KeyMsg) tea.Cmd {
	if m.mouseJunk(k) {
		return nil
	}
	s := k.String()

	if m.result != nil {
		switch s {
		case "esc", "enter", "q", "ctrl+c":
			m.result = nil
			return m.input.Focus()
		}
		return nil
	}
	if m.wiz != nil {
		return m.wizardKey(k)
	}
	if m.shell != nil {
		return m.shellKey(k)
	}
	if m.react != nil {
		return m.reactKey(k)
	}
	if m.files != nil {
		return m.filesKey(k)
	}
	if m.delArm != "" && !(m.focus == focusSelect && (isDeleteKey(s) || s == "enter")) {
		m.disarm()
		if s == "esc" {
			return nil // cancel only; the message stays selected
		}
	}
	if s == "ctrl+c" {
		switch {
		case m.sw != nil:
			m.sw = nil
		case m.input.Value() != "":
			m.input.Reset()
			m.clearSugs()
		default:
			return tea.Quit
		}
		return nil
	}
	if !m.ready {
		return nil
	}
	if m.sw != nil {
		return m.switcherKey(k)
	}

	switch s {
	case "ctrl+k":
		return m.openSwitcher()
	case "ctrl+l":
		return m.lockNow()
	case "ctrl+t":
		return m.cycleTeam()
	case "alt+up", "ctrl+p":
		return m.stepChannel(-1, false)
	case "alt+down", "ctrl+n":
		return m.stepChannel(1, false)
	case "alt+a":
		return m.stepChannel(1, true)
	case "alt+u":
		return m.toggleUnreads()
	case "pgup":
		m.p().vp.HalfPageUp()
		return m.maybeLoadOlder()
	case "pgdown":
		m.p().vp.HalfPageDown()
		return nil
	case "f1":
		m.showHelp = !m.showHelp
		return nil
	case "esc":
		switch {
		case m.showHelp:
			m.showHelp = false
		case m.textSel != nil:
			m.textSel = nil
		case len(m.sugs) > 0:
			m.clearSugs()
		case m.focus != focusInput:
			m.focus = focusInput
			m.syncSideCursor()
			m.refresh(false)
			return m.input.Focus()
		case m.active == paneThread:
			m.closeThread()
		}
		return nil
	case "tab":
		if len(m.sugs) > 0 && m.focus == focusInput {
			m.acceptSug()
			return nil
		}
		return m.cycleFocus(1)
	case "shift+tab":
		return m.cycleFocus(-1)
	}

	switch m.focus {
	case focusSidebar:
		switch s {
		case "up", "k":
			m.moveSideCursor(-1)
			return nil
		case "down", "j":
			m.moveSideCursor(1)
			return nil
		case "enter", "right", "l":
			if m.sideCursor < len(m.rows) {
				if r := m.rows[m.sideCursor]; r.item != nil {
					return m.switchTo(r.item.ch.Id)
				}
			}
			return nil
		}
		// typing anything else jumps back to the input
		m.focus = focusInput
		m.syncSideCursor()
		return tea.Batch(m.input.Focus(), m.typeInto(k))

	case focusSelect:
		pn := m.p()
		n := len(pn.visible)
		switch s {
		case "up", "k":
			if pn.sel > 0 {
				pn.sel--
			} else if cmd := m.maybeLoadOlder(); cmd != nil {
				return cmd
			}
			m.refresh(false)
			m.scrollToSel()
			return nil
		case "down", "j":
			if pn.sel < n-1 {
				pn.sel++
				m.refresh(false)
				m.scrollToSel()
				return nil
			}
			m.focus = focusInput
			m.refresh(true)
			return m.input.Focus()
		case "o":
			if pn.sel >= 0 && pn.sel < n {
				return m.openFilesCmd(pn.visible[pn.sel])
			}
			return nil
		case "c":
			if pn.sel >= 0 && pn.sel < n {
				return copyCmd(pn.visible[pn.sel].Message)
			}
			return nil
		case "e", "+":
			if pn.sel >= 0 && pn.sel < n {
				return m.openReact(pn.visible[pn.sel])
			}
			return nil
		case "d", "đ", "D", "Đ", "backspace", "delete":
			if pn.sel >= 0 && pn.sel < n {
				return m.deleteKey(pn.visible[pn.sel], false)
			}
			return nil
		case "y", "w":
			if pn.sel >= 0 && pn.sel < n && pn.visible[pn.sel].Id != "" {
				url := m.permalink(pn.visible[pn.sel])
				if s == "y" {
					return copyCmd(url)
				}
				return m.openURLCmd(url)
			}
			return nil
		case "enter", "r":
			if m.delArm != "" && pn.sel >= 0 && pn.sel < n {
				return m.deleteKey(pn.visible[pn.sel], true)
			}
			if pn.sel >= 0 && pn.sel < n {
				if m.active == paneThread {
					m.focus = focusInput
					m.refresh(true)
					return m.input.Focus()
				}
				return m.openThread(pn.visible[pn.sel])
			}
			return nil
		}
		m.focus = focusInput
		m.refresh(true)
		return tea.Batch(m.input.Focus(), m.typeInto(k))
	}

	// focusInput
	if s == "enter" {
		if shiftHeld() {
			return m.typeInto(tea.KeyMsg{Type: tea.KeyCtrlJ})
		}
		// works in every terminal: a trailing backslash turns Enter into a newline
		if v := m.input.Value(); strings.HasSuffix(v, "\\") {
			m.input.SetValue(strings.TrimSuffix(v, "\\") + "\n")
			m.input.CursorEnd()
			return nil
		}
	}
	if len(m.sugs) > 0 {
		switch s {
		case "up":
			m.sugIdx = (m.sugIdx - 1 + len(m.sugs)) % len(m.sugs)
			return nil
		case "down":
			m.sugIdx = (m.sugIdx + 1) % len(m.sugs)
			return nil
		case "enter":
			// a complete slash command runs directly; otherwise accept the pick
			if !(strings.HasPrefix(m.input.Value(), "/") && m.sugs[m.sugIdx].insert == strings.TrimSpace(m.input.Value())) {
				m.acceptSug()
				return nil
			}
		}
	}
	switch s {
	case "enter":
		return m.send()
	case "ctrl+v":
		return clipboardImageCmd()
	case "backspace":
		if m.input.Value() == "" && len(m.attach) > 0 {
			m.dropAttachment()
			return nil
		}
	case "up":
		if pn := m.p(); m.input.Value() == "" && len(pn.visible) > 0 {
			m.focus = focusSelect
			pn.sel = len(pn.visible) - 1
			m.input.Blur()
			m.refresh(false)
			m.scrollToSel()
			return nil
		}
	}
	return m.typeInto(k)
}

// mouseReport matches a whole SGR mouse report that lost its ESC.
var mouseReport = regexp.MustCompile(`\[?<\d+;\d+;\d+[Mm]`)

// mouseJunk drops pieces of mouse reports that reach us as keys. Fast wheel
// scrolling can split a report ("ESC [<65;75;52M") across two reads; the
// input parser then sees Alt+[ followed by plain text. Such fragments
// arrive within milliseconds of each other, which is how they are told
// apart from real typing.
func (m *Model) mouseJunk(k tea.KeyMsg) bool {
	if k.Type != tea.KeyRunes || k.Paste {
		return false
	}
	text := string(k.Runes)
	switch {
	case k.Alt && text == "[":
		m.junkAt = time.Now()
		return true
	case mouseReport.MatchString(text) && strings.Trim(mouseReport.ReplaceAllString(text, ""), " ") == "":
		m.junkAt = time.Now()
		return true
	case time.Since(m.junkAt) < 150*time.Millisecond && strings.Trim(text, "[<0123456789;Mm") == "":
		m.junkAt = time.Now()
		return true
	}
	return false
}

func (m *Model) typeInto(k tea.KeyMsg) tea.Cmd {
	if k.Paste {
		// Cmd+V with only an image on the clipboard yields an empty paste
		if len(k.Runes) == 0 {
			return clipboardImageCmd()
		}
		// files dragged in from Finder arrive as pasted paths
		if paths := pastedPaths(string(k.Runes)); len(paths) > 0 {
			for _, p := range paths {
				m.addAttachment(p, false)
			}
			return nil
		}
	}
	before := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	after := m.input.Value()
	if after == before {
		return cmd
	}
	cmds := []tea.Cmd{cmd, m.updateSugs()}
	if after != "" && !strings.HasPrefix(after, "/") && time.Since(m.lastTyping) > 3*time.Second {
		m.lastTyping = time.Now()
		root := ""
		if m.active == paneThread && m.thread != nil {
			root = m.thread.root
		}
		go m.c.Typing(m.cur, root)
	}
	return tea.Batch(cmds...)
}

func (m *Model) scrollToSel() {
	pn := m.p()
	if pn.sel < 0 || pn.sel >= len(pn.visible) {
		return
	}
	for _, sp := range pn.spans {
		if sp.post != pn.visible[pn.sel] {
			continue
		}
		if sp.start < pn.vp.YOffset {
			pn.vp.SetYOffset(sp.start)
		} else if sp.end > pn.vp.YOffset+pn.vp.Height {
			pn.vp.SetYOffset(sp.end - pn.vp.Height)
		}
		return
	}
}

// cycleFocus moves through sidebar -> channel -> thread (when shown side by
// side) and back; dir is 1 for Tab, -1 for Shift+Tab.
func (m *Model) cycleFocus(dir int) tea.Cmd {
	stops := []int{0, 1} // 0 sidebar, 1 channel or the only pane
	if m.geo().split {
		stops = append(stops, 2)
	}
	cur := 1
	switch {
	case m.focus == focusSidebar:
		cur = 0
	case m.geo().split && m.active == paneThread:
		cur = 2
	}
	next := stops[((cur+dir)%len(stops)+len(stops))%len(stops)]
	if next == 0 {
		m.focus = focusSidebar
		m.input.Blur()
		m.refresh(false)
		return nil
	}
	m.focus = focusInput
	m.syncSideCursor()
	var cmd tea.Cmd
	if m.geo().split {
		cmd = m.setActive(paneKind(next - 1))
	}
	m.refresh(false)
	return tea.Batch(cmd, m.input.Focus())
}

func (m *Model) maybeLoadOlder() tea.Cmd {
	if m.active != paneChannel || !m.p().vp.AtTop() {
		return nil
	}
	b := m.bufs[m.cur]
	if b == nil || !b.loaded || !b.more || b.loading {
		return nil
	}
	oldest := ""
	for _, p := range b.posts {
		if p.Id != "" {
			oldest = p.Id
			break
		}
	}
	if oldest == "" {
		return nil
	}
	b.loading = true
	m.setStatus(tr("Loading older messages…"), false)
	return m.postsCmd(m.cur, oldest)
}

// ---- mouse ----

func (m *Model) handleMouse(ev tea.MouseMsg) tea.Cmd {
	if !m.ready || m.sw != nil || m.showHelp || m.wiz != nil || m.result != nil || m.shell != nil || m.react != nil || m.files != nil {
		return nil
	}
	g := m.geo()
	inSide := ev.X < g.sideW
	k := g.paneAt(ev.X)
	pn, pg := m.panes[k], g.panes[k]

	// a drag in progress: extend it, or finish it on release
	if s := m.textSel; s != nil && s.pressX >= 0 {
		switch ev.Action {
		case tea.MouseActionMotion:
			to := m.cellAt(s.pane, ev.X, ev.Y)
			if to != s.from {
				s.moved = true
			}
			s.to = to
			return nil
		case tea.MouseActionRelease:
			x, y := s.pressX, s.pressY
			s.pressX = -1
			if s.moved {
				if text := m.selectedText(); text != "" {
					return copyCmd(text)
				}
				return nil
			}
			m.textSel = nil
			return m.clickPane(s.pane, x, y)
		}
	}

	switch ev.Button {
	case tea.MouseButtonWheelUp:
		if inSide {
			m.sideOffset = max(m.sideOffset-3, 0)
			return nil
		}
		pn.vp.ScrollUp(3)
		if k == paneChannel && m.active == paneChannel {
			return m.maybeLoadOlder()
		}
		return nil
	case tea.MouseButtonWheelDown:
		if inSide {
			m.sideOffset = min(m.sideOffset+3, max(len(m.rows)-g.sideRows, 0))
			return nil
		}
		pn.vp.ScrollDown(3)
		return nil
	case tea.MouseButtonLeft:
		if ev.Action != tea.MouseActionPress {
			return nil
		}
	default:
		return nil
	}

	m.textSel = nil
	if inSide {
		switch ev.Y {
		case 1:
			return m.cycleTeam()
		case 2:
			return m.toggleUnreads()
		}
		i := m.sideOffset + ev.Y - g.sideTop
		if i < 0 || i >= len(m.rows) {
			return nil
		}
		switch r := m.rows[i]; {
		case r.item != nil:
			return m.switchTo(r.item.ch.Id)
		case r.cat != "":
			m.toggleCategory(r.cat)
		}
		return nil
	}
	if ev.Y >= pg.vpTop && ev.Y < pg.vpTop+pn.vp.Height {
		// wait for the release to tell a click from a drag
		at := m.cellAt(k, ev.X, ev.Y)
		m.textSel = &textSel{pane: k, from: at, to: at, pressX: ev.X, pressY: ev.Y}
		return nil
	}
	return m.clickPane(k, ev.X, ev.Y)
}

// clickPane handles a plain click inside a pane: open links, select a
// message (a second click opens its thread) or focus the input.
func (m *Model) clickPane(k paneKind, x, y int) tea.Cmd {
	g := m.geo()
	pn, pg := m.panes[k], g.panes[k]
	activate := m.setActive(k)
	if y >= pg.vpTop && y < pg.vpTop+pn.vp.Height {
		line := pn.vp.YOffset + y - pg.vpTop - m.padTop(k)
		if url := pn.links[line]; url != "" {
			return tea.Batch(activate, m.openURLCmd(url))
		}
		for _, sp := range pn.spans {
			if line < sp.start || line >= sp.end {
				continue
			}
			idx := -1
			for i, p := range pn.visible {
				if p == sp.post {
					idx = i
				}
			}
			if idx < 0 {
				return activate
			}
			if m.focus == focusSelect && pn.sel == idx && k == paneChannel {
				return m.openThread(sp.post)
			}
			m.focus = focusSelect
			pn.sel = idx
			m.input.Blur()
			m.refresh(false)
			return nil
		}
		return activate
	}
	if y >= pg.inputTop {
		m.focus = focusInput
		m.refresh(false)
		return tea.Batch(activate, m.input.Focus())
	}
	return activate
}

// ---- autocomplete ----

type suggestion struct {
	insert string
	label  string
	hint   string
}

type command struct {
	name, args, help string
}

// help texts are English source strings, translated when shown
var localCommands = []command{
	{"/msg", "@user", "Same as /dm"},
	{"/open", "", "Open the current channel or thread in the browser"},
	{"/link", "", "Copy the web link of the current channel or thread"},
	{"/call", "", "Join or start a call in the browser"},
	{"/upload", "[path]", "Pick files to attach in a file browser"},
	{"/lock", "", "Lock mmt now (same as Ctrl+L)"},
	{"/unreads", "", "Show only conversations with unread messages, or all again"},
	{"/help", "", "Show shortcuts"},
	{"/quit", "", "Quit mmt"},
	{"/away", "", "Set status to away (server)"},
	{"/online", "", "Set status to online (server)"},
	{"/dnd", "", "Do not disturb (server)"},
	{"/header", "text", "Edit the channel header (server)"},
	{"/join", "~channel", "Join a channel (server)"},
	{"/leave", "", "Leave the current channel (server)"},
}

func (m *Model) clearSugs() {
	m.sugs = nil
	m.sugIdx = 0
	m.sugFor = ""
}

// lastToken returns the word being typed at the end of the input.
func lastToken(v string) string {
	i := strings.LastIndexAny(v, " \n\t")
	return v[i+1:]
}

func (m *Model) updateSugs() tea.Cmd {
	v := m.input.Value()
	if strings.HasPrefix(v, "/") && !strings.Contains(v, "\n") {
		var sugs []suggestion
		for _, a := range actions {
			if strings.HasPrefix(a.cmd, v) {
				sugs = append(sugs, suggestion{insert: a.cmd, label: a.cmd, hint: tr(a.title)})
			}
		}
		for _, c := range localCommands {
			if strings.HasPrefix(c.name, v) {
				sugs = append(sugs, suggestion{insert: c.name, label: c.name + " " + tr(c.args), hint: tr(c.help)})
			}
		}
		if len(sugs) > 0 || !strings.Contains(v, " ") {
			m.sugs, m.sugIdx, m.sugFor = sugs, 0, v
			return nil
		}
		m.clearSugs()
		return nil
	}
	tok := lastToken(v)
	switch {
	case len(tok) >= 2 && tok[0] == '@':
		if tok == m.sugFor {
			return nil
		}
		m.sugFor = tok
		term := tok[1:]
		channel, team := m.cur, ""
		if it := m.items[channel]; it != nil {
			team = it.ch.TeamId
		}
		return func() tea.Msg {
			cx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			users, err := m.c.AutocompleteInChannel(cx, team, channel, term)
			if err != nil {
				return nil
			}
			return autoMsg{token: tok, users: users}
		}
	case len(tok) >= 2 && tok[0] == '~':
		m.sugFor = tok
		m.sugs = m.sugs[:0]
		q := strings.ToLower(tok[1:])
		for _, it := range m.items {
			if isDM(it.ch) {
				continue
			}
			if strings.Contains(strings.ToLower(it.ch.Name), q) || strings.Contains(strings.ToLower(it.label), q) {
				m.sugs = append(m.sugs, suggestion{insert: "~" + it.ch.Name, label: "~" + it.ch.Name, hint: it.label})
			}
		}
		sort.Slice(m.sugs, func(i, j int) bool { return m.sugs[i].insert < m.sugs[j].insert })
		if len(m.sugs) > maxSugs {
			m.sugs = m.sugs[:maxSugs]
		}
		m.sugIdx = 0
	default:
		m.clearSugs()
	}
	return nil
}

func (m *Model) acceptSug() {
	if len(m.sugs) == 0 {
		return
	}
	s := m.sugs[m.sugIdx]
	v := m.input.Value()
	var nv string
	if strings.HasPrefix(v, "/") && !strings.Contains(v, "\n") {
		nv = s.insert + " "
	} else {
		nv = v[:len(v)-len(lastToken(v))] + s.insert + " "
	}
	m.input.SetValue(nv)
	m.input.CursorEnd()
	m.clearSugs()
}

// ---- slash commands ----

func (m *Model) runCommand(text string) tea.Cmd {
	if strings.HasPrefix(text, "/msg") {
		text = "/dm" + strings.TrimPrefix(text, "/msg")
	}
	if a, args := m.findAction(text); a != nil {
		return m.startWizard(a, args)
	}
	name, _, _ := strings.Cut(text, " ")
	switch name {
	case "/quit", "/exit", "/q":
		return tea.Quit
	case "/help":
		m.showHelp = true
		return nil
	case "/call":
		return m.openCallCmd(m.cur)
	case "/unreads":
		return m.toggleUnreads()
	case "/lock":
		if !m.lockEnabled() {
			m.setStatus(tr("No lock set up yet; run `mmt lock` in a terminal first"), true)
			return nil
		}
		return m.lockNow()
	case "/upload":
		_, arg, _ := strings.Cut(text, " ")
		return m.startUpload(arg)
	case "/open":
		if u := m.currentURL(); u != "" {
			return m.openURLCmd(u)
		}
		return nil
	case "/link":
		if u := m.currentURL(); u != "" {
			return copyCmd(u)
		}
		return nil
	}
	channel := m.cur
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		if err := m.c.Command(cx, channel, text); err != nil {
			return statusMsg{text: name + ": " + err.Error(), err: true}
		}
		return statusMsg{text: tr("Ran ") + name}
	}
}
