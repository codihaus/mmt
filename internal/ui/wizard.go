package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattermost/mattermost/server/public/model"
)

// The management commands (/channel new, /bot new, /token revoke, …) run as
// small forms: each field is asked in turn with a hint and suggestions, so
// nobody has to remember the syntax. Arguments typed after the command fill
// fields in advance; only the missing ones are asked.

type fieldKind int

const (
	fText    fieldKind = iota
	fWord              // one word, e.g. a username
	fBool              // yes / no
	fUsers             // one or more @users
	fUser              // a single @user
	fChannel           // ~channel the user is in
	fOption            // one of the options an action loads (bots, tokens)
)

type field struct {
	key, label, hint string
	kind             fieldKind
	optional         bool
	def              string
}

type wizOption struct{ value, label string }

// action is one command. run executes on a goroutine with the collected
// values and must not touch the Model; it reports back with actionDoneMsg.
type action struct {
	cmd, title, help string
	fields           []field
	confirm          bool // destructive: show a summary and ask first
	load             func(m *Model) tea.Cmd
	run              func(m *Model, v wizVals) tea.Cmd
}

// wizVals holds field values plus context captured when the form opened
// (_team, _channel, _channelLabel, _chanByName).
type wizVals map[string]string

func (v wizVals) users() []string { return strings.Fields(v["users"]) }

type wizard struct {
	act        *action
	step       int
	vals       wizVals
	ti         textinput.Model
	sugs       []wizOption
	sugIdx     int
	sugFor     string
	options    []wizOption
	chans      map[string]string // channel name -> id, captured at start
	confirming bool
	running    bool
	err        string
}

type wizOptionsMsg struct{ options []wizOption }
type wizUsersMsg struct {
	query string
	users []*model.User
}

// actionDoneMsg is the outcome of an action, shown in a result popup.
type actionDoneMsg struct {
	title   string
	lines   []string
	copy    string         // copied to the clipboard (tokens)
	open    *model.Channel // switch to this channel
	removed string         // channel id to drop from the sidebar
	err     error
}

type resultView struct {
	title string
	lines []string
	err   bool
}

func (m *Model) findAction(text string) (*action, string) {
	var best *action
	rest := ""
	for i := range actions {
		a := &actions[i]
		if text == a.cmd || strings.HasPrefix(text, a.cmd+" ") {
			if best == nil || len(a.cmd) > len(best.cmd) {
				best, rest = a, strings.TrimSpace(strings.TrimPrefix(text, a.cmd))
			}
		}
	}
	return best, rest
}

// startWizard opens the form for an action, filling fields from args.
func (m *Model) startWizard(a *action, args string) tea.Cmd {
	w := &wizard{act: a, vals: wizVals{}, chans: map[string]string{}}
	w.vals["_team"] = m.team
	w.vals["_channel"] = m.cur
	if it := m.items[m.cur]; it != nil {
		w.vals["_channelLabel"] = symbol(it.ch) + it.label
	}
	for id, it := range m.items {
		if !isDM(it.ch) {
			w.chans[it.ch.Name] = id
		}
	}
	for _, f := range a.fields {
		if f.def != "" {
			w.vals[f.key] = f.def
		}
	}
	prefill(a, w.vals, args)
	ti := textinput.New()
	ti.Prompt = "› "
	ti.Focus()
	w.ti = ti
	m.wiz = w
	m.input.Blur()
	m.clearSugs()

	var cmds []tea.Cmd
	if a.load != nil {
		cmds = append(cmds, a.load(m))
	}
	// skip fields the arguments already answered
	w.step = 0
	for w.step < len(a.fields) && args != "" && w.vals[a.fields[w.step].key] != "" {
		w.step++
	}
	if args == "" {
		w.step = 0
	}
	if w.step >= len(a.fields) {
		if a.confirm {
			w.confirming = true
		} else {
			cmds = append(cmds, m.runWizard())
		}
	} else {
		m.loadField()
	}
	return tea.Batch(append(cmds, textinput.Blink)...)
}

// prefill maps "/cmd args": --flags to yes/no fields, @names to user fields,
// ~names to the channel field, single-word fields take the next word and the
// first free-text field takes the rest.
func prefill(a *action, v wizVals, args string) {
	var words, users []string
	chanArg := ""
	flags := map[string]bool{}
	for _, t := range strings.Fields(args) {
		switch {
		case strings.HasPrefix(t, "--"):
			flags[strings.TrimPrefix(t, "--")] = true
		case strings.HasPrefix(t, "@"):
			users = append(users, t)
		case strings.HasPrefix(t, "~"):
			chanArg = t
		default:
			words = append(words, t)
		}
	}
	textDone := false
	for _, f := range a.fields {
		switch f.kind {
		case fBool:
			if flags[f.key] {
				v[f.key] = "yes"
			}
		case fUsers:
			if len(users) > 0 {
				v[f.key] = strings.Join(users, " ")
			}
		case fUser:
			if len(users) > 0 {
				v[f.key] = users[0]
			} else if len(words) > 0 {
				v[f.key] = "@" + strings.TrimPrefix(words[0], "@")
				words = words[1:]
			}
		case fChannel:
			if chanArg != "" {
				v[f.key] = chanArg
			}
		case fWord, fOption:
			if len(words) > 0 {
				v[f.key], words = words[0], words[1:]
			}
		case fText:
			if !textDone && len(words) > 0 {
				v[f.key] = strings.Join(words, " ")
				words, textDone = nil, true
			}
		}
	}
}

func (w *wizard) field() field { return w.act.fields[w.step] }

// loadField puts the current field's value into the input.
func (m *Model) loadField() {
	w := m.wiz
	w.err = ""
	w.sugs, w.sugFor = nil, ""
	f := w.field()
	val := w.vals[f.key]
	if f.kind == fBool {
		// show the default as a hint, not as text to edit around
		val = ""
	}
	w.ti.SetValue(val)
	w.ti.CursorEnd()
	switch f.kind {
	case fBool:
		w.ti.Placeholder = "y/N"
		if w.vals[f.key] == "yes" {
			w.ti.Placeholder = "Y/n"
		}
	case fUsers:
		w.ti.Placeholder = "@alice @bob"
	case fUser:
		w.ti.Placeholder = "@username"
	case fChannel:
		w.ti.Placeholder = "~channel"
	default:
		w.ti.Placeholder = ""
	}
	m.wizSuggest()
}

func lastWord(s string) string {
	f := strings.Fields(s)
	if len(f) == 0 || strings.HasSuffix(s, " ") {
		return ""
	}
	return f[len(f)-1]
}

// wizSuggest refreshes suggestions for the current field; user lookups go to
// the server and come back as wizUsersMsg.
func (m *Model) wizSuggest() tea.Cmd {
	w := m.wiz
	f := w.field()
	q := lastWord(w.ti.Value())
	if f.kind == fUser {
		q = strings.TrimSpace(w.ti.Value())
	}
	switch f.kind {
	case fUsers, fUser:
		term := strings.TrimPrefix(q, "@")
		if term == "" {
			w.sugs = nil
			return nil
		}
		if q == w.sugFor {
			return nil
		}
		w.sugFor = q
		return func() tea.Msg {
			cx, cancel := ctx()
			defer cancel()
			users, _ := m.c.Autocomplete(cx, term)
			return wizUsersMsg{query: q, users: users}
		}
	case fChannel:
		w.sugs = w.sugs[:0]
		term := strings.TrimPrefix(strings.TrimSpace(w.ti.Value()), "~")
		type sc struct {
			o wizOption
			s int
		}
		var list []sc
		for name, id := range w.chans {
			it := m.items[id]
			if it == nil {
				continue
			}
			if s := max(matchScore(term, it.label), matchScore(term, name)); s > 0 {
				list = append(list, sc{wizOption{value: "~" + name, label: symbol(it.ch) + it.label}, s})
			}
		}
		sort.Slice(list, func(i, j int) bool {
			if list[i].s != list[j].s {
				return list[i].s > list[j].s
			}
			return list[i].o.label < list[j].o.label
		})
		for i := 0; i < len(list) && i < 6; i++ {
			w.sugs = append(w.sugs, list[i].o)
		}
	case fOption:
		w.sugs = w.sugs[:0]
		term := strings.TrimSpace(w.ti.Value())
		for _, o := range w.options {
			if matchScore(term, o.label+" "+o.value) > 0 && len(w.sugs) < 6 {
				w.sugs = append(w.sugs, o)
			}
		}
	default:
		w.sugs = nil
	}
	w.sugIdx = 0
	return nil
}

func (m *Model) acceptWizSug() {
	w := m.wiz
	if len(w.sugs) == 0 {
		return
	}
	pick := w.sugs[w.sugIdx].value
	if w.field().kind == fUsers {
		v := w.ti.Value()
		v = strings.TrimSuffix(v, lastWord(v)) + pick + " "
		w.ti.SetValue(v)
	} else {
		w.ti.SetValue(pick)
	}
	w.ti.CursorEnd()
	w.sugs, w.sugFor = nil, ""
}

// commitField validates the input for the current field.
func (m *Model) commitField() bool {
	w := m.wiz
	f := w.field()
	val := strings.TrimSpace(w.ti.Value())
	if f.kind == fBool {
		switch strings.ToLower(val) {
		case "":
			val = w.vals[f.key] // the default, or the earlier answer
			if val == "" {
				val = "no"
			}
		case "n", "no", "k", "khong", "không":
			val = "no"
		case "y", "yes", "c", "co", "có":
			val = "yes"
		default:
			w.err = tr("Type y or n")
			return false
		}
	}
	if val == "" && !f.optional {
		w.err = tr("This field is required")
		return false
	}
	if f.kind == fChannel && val != "" {
		if _, ok := w.chans[strings.TrimPrefix(val, "~")]; !ok {
			w.err = tr("Pick a channel you are in")
			return false
		}
	}
	w.vals[f.key] = val
	return true
}

func (m *Model) runWizard() tea.Cmd {
	w := m.wiz
	w.running = true
	w.confirming = false
	v := wizVals{}
	for k, val := range w.vals {
		v[k] = val
	}
	// resolve ~channel to an id now; run must not read the Model
	for _, f := range w.act.fields {
		if f.kind == fChannel {
			if id, ok := w.chans[strings.TrimPrefix(v[f.key], "~")]; ok {
				v[f.key] = id
			}
		}
	}
	return w.act.run(m, v)
}

func (m *Model) wizardKey(k tea.KeyMsg) tea.Cmd {
	w := m.wiz
	s := k.String()
	if s == "esc" || s == "ctrl+c" {
		if len(w.sugs) > 0 {
			w.sugs = nil
			return nil
		}
		m.wiz = nil
		return m.input.Focus()
	}
	if w.running {
		return nil
	}
	if w.confirming {
		switch s {
		case "enter", "y":
			return m.runWizard()
		case "n":
			m.wiz = nil
			return m.input.Focus()
		case "shift+tab":
			w.confirming = false
			w.step = len(w.act.fields) - 1
			m.loadField()
		}
		return nil
	}
	switch s {
	case "up":
		if w.sugIdx > 0 {
			w.sugIdx--
		}
		return nil
	case "down":
		if w.sugIdx < len(w.sugs)-1 {
			w.sugIdx++
		}
		return nil
	case "tab":
		m.acceptWizSug()
		return m.wizSuggest()
	case "shift+tab":
		if w.step > 0 {
			w.vals[w.field().key] = strings.TrimSpace(w.ti.Value())
			w.step--
			m.loadField()
		}
		return nil
	case "enter":
		f := w.field()
		// with suggestions open, Enter picks one for single-value fields
		if len(w.sugs) > 0 && (f.kind == fUser || f.kind == fChannel || f.kind == fOption || (f.kind == fUsers && lastWord(w.ti.Value()) != "")) {
			m.acceptWizSug()
			if f.kind == fUsers {
				return m.wizSuggest()
			}
		}
		if !m.commitField() {
			return nil
		}
		w.step++
		if w.step < len(w.act.fields) {
			m.loadField()
			return m.wizSuggest()
		}
		if w.act.confirm {
			w.confirming = true
			return nil
		}
		return m.runWizard()
	}
	var cmd tea.Cmd
	before := w.ti.Value()
	w.ti, cmd = w.ti.Update(k)
	if w.ti.Value() != before {
		w.err = ""
		return tea.Batch(cmd, m.wizSuggest())
	}
	return cmd
}

func (m *Model) onActionDone(msg actionDoneMsg) tea.Cmd {
	title := ""
	if m.wiz != nil {
		title = tr(m.wiz.act.title)
	}
	m.wiz = nil
	if msg.title != "" {
		title = msg.title
	}
	var cmds []tea.Cmd
	if msg.err != nil {
		m.result = &resultView{title: title, lines: []string{msg.err.Error()}, err: true}
		return m.input.Focus()
	}
	if msg.copy != "" {
		cmds = append(cmds, func() tea.Msg {
			_ = copyText(msg.copy)
			return nil
		})
	}
	if msg.removed != "" {
		cmds = append(cmds, m.removeChannel(msg.removed))
	}
	if msg.open != nil {
		if m.items[msg.open.Id] == nil {
			m.addChannel(msg.open)
		}
		cmds = append(cmds, m.switchTo(msg.open.Id), m.catsCmd(m.team))
	}
	if len(msg.lines) > 0 {
		m.result = &resultView{title: title, lines: msg.lines}
	}
	return tea.Batch(append(cmds, m.input.Focus())...)
}

// ---- drawing ----

func (m *Model) viewWizard(w int) string {
	wz := m.wiz
	bw := min(76, w-4)
	inner := bw - 4
	var lines []string
	lines = append(lines, stDim.Render(ansi.Truncate(tr(wz.act.help), inner, "…")), "")
	for i, f := range wz.act.fields {
		label := fmt.Sprintf("%-18s", tr(f.label))
		val := wz.vals[f.key]
		switch {
		case i < wz.step || wz.confirming || wz.running:
			if val == "" {
				val = stDim.Render("—")
			}
			lines = append(lines, stOK.Render("✓ ")+label+" "+ansi.Truncate(val, inner-22, "…"))
		case i == wz.step:
			opt := ""
			if f.optional {
				opt = stDim.Render(" " + tr("(optional)"))
			}
			lines = append(lines, stAccent.Bold(true).Render("› "+label)+opt)
		default:
			lines = append(lines, stDim.Render("  "+label))
		}
	}
	lines = append(lines, "")
	switch {
	case wz.running:
		lines = append(lines, stDim.Render(tr("Working…")))
	case wz.confirming:
		if wz.act.confirm {
			lines = append(lines, stErr.Render(tr("This cannot be undone.")))
		}
		lines = append(lines, stTitle.Render(tr("Enter or y to run · n or Esc to cancel · Shift+Tab to edit")))
	default:
		f := wz.field()
		if f.hint != "" {
			lines = append(lines, stDim.Render(ansi.Truncate(tr(f.hint), inner, "…")))
		}
		lines = append(lines, wz.ti.View())
		for i, s := range wz.sugs {
			text := ansi.Truncate(" "+s.label+stDim.Render("  "+s.value), inner, "…")
			if strings.HasPrefix(s.label, s.value) {
				text = ansi.Truncate(" "+s.label, inner, "…")
			}
			if i == wz.sugIdx {
				text = stSugSel.Render(text + strings.Repeat(" ", max(inner-ansi.StringWidth(text), 0)))
			}
			lines = append(lines, text)
		}
		if wz.err != "" {
			lines = append(lines, stErr.Render(wz.err))
		}
		lines = append(lines, "", stDim.Render(tr("Enter next · Tab complete · Shift+Tab back · Esc cancel")))
	}
	return stPopup.Width(bw - 2).Render(stTitle.Render(tr(wz.act.title)+"  "+stDim.Render(wz.act.cmd)) + "\n" + strings.Join(lines, "\n"))
}

func (m *Model) viewResult(w int) string {
	r := m.result
	bw := min(80, w-4)
	inner := bw - 4
	var lines []string
	for _, l := range r.lines {
		for _, part := range strings.Split(ansi.Wrap(l, inner, ""), "\n") {
			if r.err {
				part = stErr.Render(part)
			}
			lines = append(lines, part)
		}
	}
	lines = append(lines, "", stDim.Render(tr("Esc or Enter to close")))
	return stPopup.Width(bw - 2).Render(stTitle.Render(r.title) + "\n\n" + strings.Join(lines, "\n"))
}

// fmtTime formats a millisecond timestamp for result popups.
func fmtTime(ms int64) string {
	if ms == 0 {
		return "—"
	}
	return time.UnixMilli(ms).Format("2006-01-02 15:04")
}
