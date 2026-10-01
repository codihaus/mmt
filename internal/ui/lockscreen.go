package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/codihaus/mmt/internal/lock"
)

// The app lock hides everything behind a lock screen: on Ctrl+L, or after
// the configured idle time. It opens with Touch ID or the mmt passcode,
// independent of the Mattermost session, which keeps running underneath.

type lockState struct {
	ti   textinput.Model
	busy bool // Touch ID prompt or passcode check in progress
	err  string
}

type unlockMsg struct {
	ok  bool
	err error
}

func (m *Model) lockEnabled() bool { return m.cfg.Lock != lock.ModeOff }

func (m *Model) lockNow() tea.Cmd {
	if !m.lockEnabled() || m.lock != nil {
		return nil
	}
	ti := textinput.New()
	ti.Prompt = "› "
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '•'
	ti.Placeholder = tr("passcode")
	ti.Focus()
	m.lock = &lockState{ti: ti}
	m.sw, m.wiz, m.result, m.textSel = nil, nil, nil, nil
	m.input.Blur()
	return tea.Batch(textinput.Blink, m.touchIDCmd())
}

func (m *Model) touchIDCmd() tea.Cmd {
	if m.cfg.Lock != lock.ModeTouchID || !lock.TouchIDAvailable() {
		return nil
	}
	m.lock.busy = true
	return func() tea.Msg {
		ok, err := lock.TouchID(tr("unlock mmt"))
		return unlockMsg{ok: ok, err: err}
	}
}

func (m *Model) onUnlock(msg unlockMsg) tea.Cmd {
	if m.lock == nil {
		return nil
	}
	m.lock.busy = false
	if msg.ok {
		m.lock = nil
		m.lastInput = time.Now()
		m.refresh(false)
		return m.input.Focus()
	}
	if msg.err != nil {
		m.lock.err = msg.err.Error()
	} else if m.lock.ti.Value() != "" {
		m.lock.err = tr("Wrong passcode.")
	}
	m.lock.ti.Reset()
	return nil
}

func (m *Model) lockKey(k tea.KeyMsg) tea.Cmd {
	if m.mouseJunk(k) {
		return nil
	}
	l := m.lock
	switch k.String() {
	case "ctrl+c":
		return tea.Quit
	case "enter":
		if l.busy || l.ti.Value() == "" {
			return nil
		}
		l.busy, l.err = true, ""
		code := l.ti.Value()
		return func() tea.Msg {
			ok, err := lock.Verify(code)
			if err != nil {
				return unlockMsg{err: err}
			}
			if !ok {
				return unlockMsg{}
			}
			return unlockMsg{ok: true}
		}
	case "esc":
		if !l.busy {
			l.err = ""
			return m.touchIDCmd()
		}
		return nil
	}
	var cmd tea.Cmd
	l.ti, cmd = l.ti.Update(k)
	return cmd
}

func (m *Model) viewLock() string {
	l := m.lock
	lines := []string{
		stTitle.Render("mmt") + stDim.Render("  ·  "+tr("locked")),
		"",
		"   ▄▄▄   ",
		"  █   █  ",
		" ▐█████▌ ",
		" ▐██▄██▌ ",
		" ▐█████▌ ",
		"",
	}
	switch {
	case l.busy && m.cfg.Lock == lock.ModeTouchID && l.ti.Value() == "":
		lines = append(lines, tr("Touch the sensor to unlock…"), stDim.Render(tr("or type the mmt passcode")))
	default:
		hint := tr("Type the mmt passcode, then Enter")
		if m.cfg.Lock == lock.ModeTouchID {
			hint += " · " + tr("Esc for Touch ID")
		}
		lines = append(lines, stDim.Render(hint))
	}
	lines = append(lines, "", l.ti.View())
	if l.err != "" {
		lines = append(lines, stErr.Render(l.err))
	}
	box := stPopup.Padding(1, 4).Render(lipgloss.JoinVertical(lipgloss.Center, lines...))
	return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, box)
}

// checkIdleLock locks after the configured idle minutes; called every tick.
func (m *Model) checkIdleLock(now time.Time) tea.Cmd {
	if !m.lockEnabled() || m.lock != nil || m.cfg.LockAfter <= 0 || m.lastInput.IsZero() {
		return nil
	}
	if now.Sub(m.lastInput) >= time.Duration(m.cfg.LockAfter)*time.Minute {
		return m.lockNow()
	}
	return nil
}

// noticeText hides message content in notifications while locked.
func (m *Model) noticeText(title, body string) (string, string) {
	if m.lock != nil {
		return "mmt", tr("New message")
	}
	return title, strings.TrimSpace(body)
}
