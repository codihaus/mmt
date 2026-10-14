package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattermost/mattermost/server/public/model"

	"github.com/codihaus/mmt/internal/mm"
	"github.com/codihaus/mmt/internal/sound"
)

// Calls (the com.mattermost.calls plugin) need audio and video, which a
// terminal cannot do. mmt shows call posts as cards, notifies on incoming
// DM calls, and opens the channel in the web app to join.

const (
	callPostType  = "custom_calls"
	callEventPref = "custom_com.mattermost.calls_"
)

// joinCallURL opens the channel in the browser and joins its call; with no
// call running, the Calls plugin starts one and joins it.
func (m *Model) joinCallURL(channelID string) string {
	it := m.items[channelID]
	if it == nil {
		return ""
	}
	return mm.CallURL(m.c.Server(), m.teamName(it.ch.TeamId), channelID)
}

func (m *Model) openCallCmd(channelID string) tea.Cmd {
	u := m.joinCallURL(channelID)
	if u == "" {
		return nil
	}
	sound.StopRing(channelID)
	if m.activeCall(channelID) {
		m.setStatus(tr("Joining the call in the browser"), false)
	} else {
		m.setStatus(tr("Starting a call in the browser"), false)
	}
	return m.openURLCmd(u)
}

func propInt(p *model.Post, key string) int64 {
	switch v := p.GetProp(key).(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	}
	return 0
}

// callLines renders a call post as a card; the second result is the link
// opened when the card is clicked.
func (m *Model) callLines(p *model.Post) ([]string, string) {
	start, end := propInt(p, "start_at"), propInt(p, "end_at")
	status, _ := p.GetProp("call_status").(string)
	title, _ := p.GetProp("title").(string)
	head := stAccent.Bold(true).Render("[call] ") + p.Message
	if title != "" {
		head += stDim.Render(" · " + title)
	}
	if end == 0 && status != "missed" && status != "declined" {
		return []string{head, stLink.Render(tr("In progress")) + stDim.Render(tr("  ↗ click to join"))}, m.joinCallURL(p.ChannelId)
	}
	var info string
	switch {
	case status == "missed":
		info = tr("Missed call")
	case status == "declined":
		info = tr("Call declined")
	default:
		d := time.Duration(max(end-start, 0)) * time.Millisecond
		info = fmt.Sprintf(tr("Call ended · %s"), shortDuration(d))
		if n := len(propList(p, "participants")); n > 0 {
			info += fmt.Sprintf(tr(" · %d people"), n)
		}
	}
	return []string{head, stDim.Render(info)}, ""
}

func propList(p *model.Post, key string) []any {
	l, _ := p.GetProp(key).([]any)
	return l
}

func shortDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

// activeCall reports a call in progress in the channel, known from call
// events or from a loaded call post that has not ended.
func (m *Model) activeCall(channelID string) bool {
	if on, ok := m.calls[channelID]; ok {
		return on
	}
	if b := m.bufs[channelID]; b != nil {
		for i := len(b.posts) - 1; i >= 0; i-- {
			if p := b.posts[i]; p.Type == callPostType {
				return propInt(p, "end_at") == 0 && p.GetProp("call_status") != "missed"
			}
		}
	}
	return false
}

// onCallEvent handles call_start / call_end from the Calls plugin.
func (m *Model) onCallEvent(ev *model.WebSocketEvent) tea.Cmd {
	name := strings.TrimPrefix(string(ev.EventType()), callEventPref)
	data := ev.GetData()
	ch, _ := data["channelID"].(string)
	if ch == "" {
		ch = ev.GetBroadcast().ChannelId
	}
	if ch == "" {
		return nil
	}
	switch name {
	case "call_start":
		m.calls[ch] = true
		owner, _ := data["owner_id"].(string)
		if it := m.items[ch]; it != nil && owner != m.c.Me.Id && isDM(it.ch) {
			m.notify(tr("Incoming call"), fmt.Sprintf(tr("%s is calling you"), "@"+m.c.Username(owner)))
			sound.Ring(ch, m.cfg.CallSound)
		}
		return m.usersCmd([]string{owner})
	case "call_end":
		m.calls[ch] = false
		sound.StopRing(ch)
	}
	return nil
}
