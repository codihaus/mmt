package ui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattermost/mattermost/server/public/model"

	"github.com/codihaus/mmt/internal/sound"
)

// A buzz, as in Yahoo! Messenger: a message that is just "!buzz" brings the
// terminal running mmt to the front, plays a sound and shakes the screen.
// Other clients show it as plain text, and anyone can send one from them.

const (
	buzzCooldown = 5 * time.Second // a burst of buzzes counts once
	shakeFrame   = 50 * time.Millisecond
)

// shakeOffsets is how far right each frame of the shake moves the screen.
var shakeOffsets = []int{3, 0, 3, 0, 2, 0, 2, 0, 1, 0, 1, 0}

type shakeMsg struct{}

func isBuzz(text string) bool {
	return strings.EqualFold(strings.TrimSpace(text), "!buzz")
}

// onBuzz answers a buzz from someone else. Muted channels stay quiet. The
// conversation opens unless that would disturb a draft or a thread.
func (m *Model) onBuzz(p *model.Post, it *chanItem) tea.Cmd {
	if (it != nil && it.muted) || time.Since(m.buzzAt) < buzzCooldown {
		return nil
	}
	m.buzzAt = time.Now()
	sound.Buzz(m.cfg.BuzzSound)
	focusTerminal()
	var cmds []tea.Cmd
	if m.lock == nil && it != nil && p.ChannelId != m.cur && m.thread == nil && strings.TrimSpace(m.input.Value()) == "" {
		cmds = append(cmds, m.switchTo(p.ChannelId))
	}
	if m.shake == 0 {
		cmds = append(cmds, shakeTick())
	}
	m.shake = len(shakeOffsets)
	return tea.Batch(cmds...)
}

func shakeTick() tea.Cmd {
	return tea.Tick(shakeFrame, func(time.Time) tea.Msg { return shakeMsg{} })
}

// shaken moves the screen right for the current frame of a shake.
func (m *Model) shaken(s string) string {
	if m.shake == 0 {
		return s
	}
	dx := shakeOffsets[len(shakeOffsets)-m.shake]
	if dx == 0 {
		return s
	}
	pad := strings.Repeat(" ", dx)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = ansi.Truncate(pad+l, m.w, "")
	}
	return strings.Join(lines, "\n")
}
