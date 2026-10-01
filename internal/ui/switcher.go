package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattermost/mattermost/server/public/model"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

const (
	swLocalMax  = 10
	swRemoteMax = 5
	swDebounce  = 200 * time.Millisecond
)

// swEntry is one selectable row: a channel the user is in, a user to DM, or a
// public channel to join.
type swEntry struct {
	item *chanItem
	user *model.User
	ch   *model.Channel
}

type switcher struct {
	ti      textinput.Model
	res     []swEntry
	idx     int
	seq     int  // bumps on every query change; stale searches are dropped
	pending bool // server search in flight
	users   []*model.User
	chans   []*model.Channel
}

type swTickMsg struct{ seq int }
type swRemoteMsg struct {
	seq   int
	users []*model.User
	chans []*model.Channel
}
type joinedMsg struct {
	ch  *model.Channel
	err error
}

var foldT = transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)

// fold lowercases and strips diacritics so "co hoi" finds "Cơ hội" and
// "dang" finds "Đăng".
func fold(s string) string {
	s = strings.NewReplacer("đ", "d", "Đ", "d").Replace(s)
	out, _, err := transform.String(foldT, s)
	if err != nil {
		out = s
	}
	return strings.ToLower(out)
}

// matchScore ranks how well query q matches text; 0 means no match.
func matchScore(q, text string) int {
	q, t := fold(strings.TrimSpace(q)), fold(text)
	if q == "" {
		return 1
	}
	switch {
	case strings.HasPrefix(t, q):
		return 100
	case strings.Contains(" "+strings.NewReplacer("-", " ", "_", " ", ".", " ").Replace(t), " "+q):
		return 80 // start of a word
	case strings.Contains(t, q):
		return 60
	}
	all := true
	for _, tok := range strings.Fields(q) {
		if !strings.Contains(t, tok) {
			all = false
			break
		}
	}
	if all {
		return 50
	}
	// letters in order, e.g. "ttl" for "tech-lead-team"
	i := 0
	cq := strings.ReplaceAll(q, " ", "")
	for _, r := range t {
		if i < len(cq) && rune(cq[i]) == r {
			i++
		}
	}
	if i == len(cq) {
		return 20
	}
	return 0
}

func (m *Model) openSwitcher() tea.Cmd {
	ti := textinput.New()
	ti.Placeholder = tr("Type a channel or person (accents optional)…")
	ti.Prompt = "› "
	ti.Focus()
	m.sw = &switcher{ti: ti}
	m.input.Blur()
	m.filterSwitcher()
	return textinput.Blink
}

// filterSwitcher ranks joined channels locally and schedules a server search
// for people and channels not joined yet.
func (m *Model) filterSwitcher() tea.Cmd {
	sw := m.sw
	q := strings.TrimSpace(sw.ti.Value())
	type scored struct {
		it    *chanItem
		score int
	}
	var list []scored
	for _, it := range m.items {
		text := it.label + " " + it.ch.Name
		if s := matchScore(q, text); s > 0 {
			// the display name alone decides prefix/word matches
			if ls := matchScore(q, it.label); ls > s {
				s = ls
			}
			list = append(list, scored{it, s})
		}
	}
	sort.Slice(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a.score != b.score {
			return a.score > b.score
		}
		if a.it.mentions != b.it.mentions {
			return a.it.mentions > b.it.mentions
		}
		if (a.it.unread > 0) != (b.it.unread > 0) {
			return a.it.unread > 0
		}
		return a.it.ch.LastPostAt > b.it.ch.LastPostAt
	})
	if len(list) > swLocalMax {
		list = list[:swLocalMax]
	}

	sw.seq++
	sw.users, sw.chans = nil, nil
	sw.res = sw.res[:0]
	for _, s := range list {
		sw.res = append(sw.res, swEntry{item: s.it})
	}
	sw.idx = 0
	if len([]rune(q)) < 2 {
		sw.pending = false
		return nil
	}
	sw.pending = true
	seq := sw.seq
	return tea.Tick(swDebounce, func(time.Time) tea.Msg { return swTickMsg{seq: seq} })
}

func (m *Model) switcherSearchCmd(seq int, q string) tea.Cmd {
	team := m.team
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		users, _ := m.c.Autocomplete(cx, strings.TrimPrefix(q, "@"))
		var chans []*model.Channel
		if team != "" {
			chans, _ = m.c.SearchChannels(cx, team, strings.TrimPrefix(q, "~"))
		}
		return swRemoteMsg{seq: seq, users: users, chans: chans}
	}
}

// applyRemote appends server results that are not already listed.
func (m *Model) applyRemote(msg swRemoteMsg) {
	sw := m.sw
	if sw == nil || msg.seq != sw.seq {
		return
	}
	sw.pending = false
	dm := map[string]bool{}
	for _, it := range m.items {
		if it.ch.Type == model.ChannelTypeDirect {
			dm[m.c.DMPartner(it.ch)] = true
		}
	}
	shown := map[string]bool{}
	for _, e := range sw.res {
		if e.item != nil && e.item.ch.Type == model.ChannelTypeDirect {
			shown[m.c.DMPartner(e.item.ch)] = true
		}
	}
	for _, u := range msg.users {
		if u.Id == m.c.Me.Id || shown[u.Id] || u.DeleteAt > 0 {
			continue
		}
		if len(sw.users) < swRemoteMax {
			sw.users = append(sw.users, u)
		}
	}
	for _, ch := range msg.chans {
		if m.items[ch.Id] != nil || ch.DeleteAt > 0 || ch.Type != model.ChannelTypeOpen {
			continue
		}
		if len(sw.chans) < swRemoteMax {
			sw.chans = append(sw.chans, ch)
		}
	}
	for _, u := range sw.users {
		sw.res = append(sw.res, swEntry{user: u})
	}
	for _, ch := range sw.chans {
		sw.res = append(sw.res, swEntry{ch: ch})
	}
}

func (m *Model) switcherKey(k tea.KeyMsg) tea.Cmd {
	sw := m.sw
	switch k.String() {
	case "esc", "ctrl+k":
		m.sw = nil
		return m.input.Focus()
	case "up", "ctrl+p":
		if sw.idx > 0 {
			sw.idx--
		}
		return nil
	case "down", "ctrl+n", "tab":
		if sw.idx < len(sw.res)-1 {
			sw.idx++
		}
		return nil
	case "enter":
		if len(sw.res) == 0 {
			return nil
		}
		e := sw.res[sw.idx]
		m.sw = nil
		switch {
		case e.item != nil:
			return m.switchTo(e.item.ch.Id)
		case e.user != nil:
			name := e.user.Username
			return func() tea.Msg {
				cx, cancel := ctx()
				defer cancel()
				ch, err := m.c.OpenDM(cx, name)
				return dmMsg{ch: ch, err: err}
			}
		case e.ch != nil:
			ch := e.ch
			return func() tea.Msg {
				cx, cancel := ctx()
				defer cancel()
				return joinedMsg{ch: ch, err: m.c.Join(cx, ch.Id)}
			}
		}
		return nil
	}
	var cmd tea.Cmd
	before := sw.ti.Value()
	sw.ti, cmd = sw.ti.Update(k)
	if sw.ti.Value() != before {
		return tea.Batch(cmd, m.filterSwitcher())
	}
	return cmd
}

func (m *Model) viewSwitcher(w int) string {
	sw := m.sw
	bw := min(68, w-4)
	inner := bw - 4
	lines := []string{sw.ti.View(), ""}
	row := func(i int, left, right string) string {
		rw := ansi.StringWidth(right)
		left = ansi.Truncate(left, inner-rw-1, "…")
		text := left + strings.Repeat(" ", max(inner-rw-ansi.StringWidth(left), 1)) + right
		if i == sw.idx {
			return stSugSel.Render(text)
		}
		return text
	}
	section := ""
	for i, e := range sw.res {
		switch {
		case e.item != nil:
			it := e.item
			badge := ""
			switch {
			case it.mentions > 0:
				badge = stBadge.Render(fmt.Sprintf(" %d ", it.mentions))
			case it.unread > 0 && !it.muted:
				badge = stAccent.Render("●")
			}
			left := symbol(it.ch) + it.label
			if it.team != "" && len(m.teams) > 1 && !isDM(it.ch) {
				left += stDim.Render("  " + it.team)
			}
			lines = append(lines, row(i, left, badge))
		case e.user != nil:
			if section != "user" {
				section = "user"
				lines = append(lines, "", stSection.Render(tr("PEOPLE · Enter to message")))
			}
			lines = append(lines, row(i, "@ "+e.user.Username+stDim.Render("  "+fullName(e.user)), ""))
		case e.ch != nil:
			if section != "chan" {
				section = "chan"
				lines = append(lines, "", stSection.Render(tr("OTHER CHANNELS · Enter to join")))
			}
			lines = append(lines, row(i, "# "+e.ch.DisplayName+stDim.Render("  ~"+e.ch.Name), ""))
		}
	}
	switch {
	case sw.pending:
		lines = append(lines, "", stDim.Render(tr("Searching the server…")))
	case len(sw.res) == 0:
		lines = append(lines, stDim.Render(tr("No channels or people found.")))
	}
	return stPopup.Width(bw - 2).Render(stTitle.Render(keys(tr("Switch channel (Ctrl+K)"))) + "\n" + strings.Join(lines, "\n"))
}
