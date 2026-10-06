package ui

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattermost/mattermost/server/public/model"
)

// The reaction picker: pick from the usual few, or type part of a name to
// search every emoji the server knows. Picking one you already reacted with
// takes it back, like clicking it on the web.

var quickEmoji = []string{"+1", "white_check_mark", "grinning", "heart", "tada", "eyes", "fire", "joy"}

// emojiNames is every system emoji name, sorted, without skin-tone variants.
var emojiNames = func() []string {
	names := make([]string, 0, len(model.SystemEmojis))
	for n := range model.SystemEmojis {
		if !strings.Contains(n, "_tone") {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}()

const maxEmojiResults = 8

type reactPicker struct {
	post *model.Post
	ti   textinput.Model
	res  []string
	idx  int
}

func (m *Model) openReact(p *model.Post) tea.Cmd {
	if p == nil || p.Id == "" {
		return nil
	}
	ti := textinput.New()
	ti.Placeholder = tr("Type to search, e.g. thumbs, fire, party")
	ti.Prompt = "› "
	ti.Focus()
	m.react = &reactPicker{post: p, ti: ti, res: quickEmoji}
	m.input.Blur()
	return textinput.Blink
}

// searchEmoji lists names that start with q first, then names that contain it.
func searchEmoji(q string) []string {
	q = strings.ToLower(strings.Trim(strings.TrimSpace(q), ":"))
	if q == "" {
		return quickEmoji
	}
	var starts, contains []string
	for _, n := range emojiNames {
		switch {
		case strings.HasPrefix(n, q):
			starts = append(starts, n)
		case strings.Contains(n, q):
			contains = append(contains, n)
		}
	}
	res := append(starts, contains...)
	return res[:min(len(res), maxEmojiResults)]
}

func (m *Model) reactKey(k tea.KeyMsg) tea.Cmd {
	r := m.react
	switch k.String() {
	case "esc", "ctrl+c":
		return m.closeReact()
	case "up":
		if len(r.res) > 0 {
			r.idx = (r.idx - 1 + len(r.res)) % len(r.res)
		}
		return nil
	case "down", "tab":
		if len(r.res) > 0 {
			r.idx = (r.idx + 1) % len(r.res)
		}
		return nil
	case "enter":
		if r.idx < len(r.res) {
			return m.pickReaction(r.post, r.res[r.idx])
		}
		return nil
	}
	// 1-8 picks from the list while nothing is typed
	if s := k.String(); r.ti.Value() == "" && len(s) == 1 && s >= "1" && s <= "8" {
		if i := int(s[0] - '1'); i < len(r.res) {
			return m.pickReaction(r.post, r.res[i])
		}
		return nil
	}
	var cmd tea.Cmd
	r.ti, cmd = r.ti.Update(k)
	r.res, r.idx = searchEmoji(r.ti.Value()), 0
	return cmd
}

// pickReaction closes the picker and adds the emoji, or removes it when it
// is already yours.
func (m *Model) pickReaction(p *model.Post, name string) tea.Cmd {
	return tea.Batch(m.closeReact(), m.reactCmd(p, name))
}

// closeReact goes back to where the picker was opened from: the selected
// message stays selected, so several messages can be reacted to in a row.
func (m *Model) closeReact() tea.Cmd {
	m.react = nil
	if m.focus == focusInput {
		return m.input.Focus()
	}
	return nil
}

func (m *Model) hasReacted(p *model.Post, name string) bool {
	if p.Metadata == nil {
		return false
	}
	for _, r := range p.Metadata.Reactions {
		if r.EmojiName == name && r.UserId == m.c.Me.Id {
			return true
		}
	}
	return false
}

func (m *Model) reactCmd(p *model.Post, name string) tea.Cmd {
	on := !m.hasReacted(p, name)
	postID := p.Id
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		if err := m.c.React(cx, postID, name, on); err != nil {
			return statusMsg{text: tr("Cannot react: ") + err.Error(), err: true}
		}
		return nil
	}
}

// reactShortcut is "+:emoji:", the web app's way to react to the latest
// message without leaving the keyboard.
var reactShortcut = regexp.MustCompile(`^\+:([a-z0-9_+\-]+):$`)

func (m *Model) reactToLatest(name string) tea.Cmd {
	if _, ok := model.SystemEmojis[name]; !ok {
		m.setStatus(fmt.Sprintf(tr("Unknown emoji :%s:"), name), true)
		return nil
	}
	vis := m.p().visible
	for i := len(vis) - 1; i >= 0; i-- {
		if vis[i].Id != "" {
			return m.reactCmd(vis[i], name)
		}
	}
	return nil
}

func (m *Model) viewReact(w int) string {
	r := m.react
	bw := min(52, w-4)
	inner := bw - 4
	who := m.c.Username(r.post.UserId)
	snippet := strings.Join(strings.Fields(r.post.Message), " ")
	head := stTitle.Render(tr("React to ")+"@"+who) + stDim.Render("  "+snippet)
	lines := []string{ansi.Truncate(head, inner, "…"), "", r.ti.View(), ""}
	for i, n := range r.res {
		label := emojiGlyph(n) + "  :" + n + ":"
		if m.hasReacted(r.post, n) {
			label += stDim.Render(tr("  (yours, Enter removes it)"))
		}
		if r.ti.Value() == "" {
			label = stDim.Render(fmt.Sprintf("%d ", i+1)) + label
		}
		label = ansi.Truncate(label, inner, "…")
		if i == r.idx {
			label = stSugSel.Render(label + strings.Repeat(" ", max(inner-ansi.StringWidth(label), 0)))
		}
		lines = append(lines, label)
	}
	if len(r.res) == 0 {
		lines = append(lines, stDim.Render(tr("No emoji with that name")))
	}
	return stPopup.Width(bw - 2).Render(strings.Join(lines, "\n"))
}
