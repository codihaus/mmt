package ui

import (
	"encoding/json"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattermost/mattermost/server/public/model"

	"github.com/codihaus/mmt/internal/mm"
)

func eventPost(ev *model.WebSocketEvent) *model.Post {
	raw, _ := ev.GetData()["post"].(string)
	if raw == "" {
		return nil
	}
	var p model.Post
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil
	}
	return mm.CleanPost(&p)
}

func (m *Model) handleEvent(ev *model.WebSocketEvent) tea.Cmd {
	data := ev.GetData()
	if strings.HasPrefix(string(ev.EventType()), callEventPref) {
		return m.onCallEvent(ev)
	}
	switch ev.EventType() {
	case model.WebsocketEventPosted, model.WebsocketEventEphemeralMessage:
		p := eventPost(ev)
		if p == nil {
			return nil
		}
		return m.onPosted(p, data)

	case model.WebsocketEventPostEdited:
		p := eventPost(ev)
		if p == nil {
			return nil
		}
		replace := func(list []*model.Post) {
			for i, q := range list {
				if q.Id == p.Id {
					list[i] = p
				}
			}
		}
		if b := m.bufs[p.ChannelId]; b != nil {
			replace(b.posts)
		}
		if m.thread != nil {
			replace(m.thread.posts)
		}
		m.refresh(false)

	case model.WebsocketEventPostDeleted:
		p := eventPost(ev)
		if p == nil {
			return nil
		}
		if b := m.bufs[p.ChannelId]; b != nil {
			b.posts = remove(b.posts, p.Id)
		}
		if m.thread != nil {
			if p.Id == m.thread.root {
				m.closeThread()
				m.setStatus(tr("The thread was deleted"), false)
			} else {
				m.thread.posts = remove(m.thread.posts, p.Id)
			}
		}
		m.refresh(false)

	case model.WebsocketEventReactionAdded, model.WebsocketEventReactionRemoved:
		m.onReaction(ev, ev.EventType() == model.WebsocketEventReactionAdded)

	case model.WebsocketEventTyping:
		uid, _ := data["user_id"].(string)
		parent, _ := data["parent_id"].(string)
		ch := ev.GetBroadcast().ChannelId
		if uid == "" || uid == m.c.Me.Id {
			return nil
		}
		k := ch + "|" + parent
		if m.typing[k] == nil {
			m.typing[k] = map[string]time.Time{}
		}
		m.typing[k][uid] = time.Now().Add(6 * time.Second)
		return m.usersCmd([]string{uid})

	case "channel_viewed": // older servers
		if id, _ := data["channel_id"].(string); id != "" {
			if it := m.items[id]; it != nil {
				it.unread, it.mentions = 0, 0
			}
			m.unreadsChanged()
		}

	case model.WebsocketEventMultipleChannelsViewed:
		if times, ok := data["channel_times"].(map[string]any); ok {
			for id := range times {
				if it := m.items[id]; it != nil {
					it.unread, it.mentions = 0, 0
				}
			}
			m.unreadsChanged()
		}

	case model.WebsocketEventDirectAdded, model.WebsocketEventGroupAdded, model.WebsocketEventChannelCreated:
		if id := ev.GetBroadcast().ChannelId; id != "" && m.items[id] == nil {
			return m.channelCmd(id)
		}

	case model.WebsocketEventSidebarCategoryCreated, model.WebsocketEventSidebarCategoryUpdated,
		model.WebsocketEventSidebarCategoryDeleted, model.WebsocketEventSidebarCategoryOrderUpdated:
		team := ev.GetBroadcast().TeamId
		if t, _ := data["team_id"].(string); t != "" {
			team = t
		}
		return m.catsCmd(team)

	case model.WebsocketEventUserRemoved:
		uid, _ := data["user_id"].(string)
		if uid == m.c.Me.Id || ev.GetBroadcast().UserId == m.c.Me.Id {
			id, _ := data["channel_id"].(string)
			if id == "" {
				id = ev.GetBroadcast().ChannelId
			}
			return m.removeChannel(id)
		}

	case model.WebsocketEventChannelDeleted:
		if id, _ := data["channel_id"].(string); id != "" {
			return m.removeChannel(id)
		}

	case model.WebsocketEventUserAdded:
		if uid, _ := data["user_id"].(string); uid == m.c.Me.Id {
			if id := ev.GetBroadcast().ChannelId; id != "" && m.items[id] == nil {
				return m.channelCmd(id)
			}
		}
	}
	return nil
}

func (m *Model) onPosted(p *model.Post, data map[string]any) tea.Cmd {
	var cmds []tea.Cmd
	if b := m.bufs[p.ChannelId]; b != nil && b.loaded {
		b.posts = upsert(b.posts, p)
	}
	if m.thread != nil && (p.RootId == m.thread.root || p.Id == m.thread.root) {
		m.thread.posts = upsert(m.thread.posts, p)
	}
	// someone who was typing has now posted
	delete(m.typing[p.ChannelId+"|"+p.RootId], p.UserId)

	it := m.items[p.ChannelId]
	if it == nil {
		cmds = append(cmds, m.channelCmd(p.ChannelId))
	} else {
		it.ch.LastPostAt = p.CreateAt
		if isDM(it.ch) {
			m.buildRows()
		}
	}
	cmds = append(cmds, m.usersCmd([]string{p.UserId}))

	mine := p.UserId == m.c.Me.Id
	if p.ChannelId == m.cur {
		if !mine && time.Since(m.lastView[m.cur]) > 3*time.Second {
			cmds = append(cmds, m.viewCmd(m.cur, ""))
		}
		m.refresh(false)
		return tea.Batch(cmds...)
	}
	// call posts are announced by the call_start event instead
	if mine || strings.HasPrefix(p.Type, "system_") || p.Type == callPostType {
		return tea.Batch(cmds...)
	}

	mentioned := false
	if raw, _ := data["mentions"].(string); raw != "" {
		var ids []string
		if json.Unmarshal([]byte(raw), &ids) == nil {
			for _, id := range ids {
				if id == m.c.Me.Id {
					mentioned = true
				}
			}
		}
	}
	dm := it != nil && isDM(it.ch) && !it.muted
	if it != nil {
		it.unread++
		if mentioned || dm {
			it.mentions++
		}
		m.unreadsChanged()
	}
	if mentioned || dm {
		sender, _ := data["sender_name"].(string)
		title := sender
		if it != nil && !dm {
			title = sender + tr(" in ") + it.label
		}
		m.notify(title, p.Message)
	}
	return tea.Batch(cmds...)
}
