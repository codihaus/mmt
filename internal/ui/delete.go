package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattermost/mattermost/server/public/model"
)

// d on a selected message of yours arms it: it turns red and the footer
// asks. Enter deletes it, any other key cancels. The message disappears at
// once and comes back if the server refuses. The selection stays put, so
// d Enter d Enter clears several in a row.
//
// Pressing d again while armed changes nothing, which keeps Vietnamese
// Telex harmless: it may turn a second d into ⌫ đ.

type deletedMsg struct {
	posts []*model.Post // what was taken off screen, to put back on failure
	err   error
}

func isDeleteKey(s string) bool {
	switch s {
	case "d", "đ", "D", "Đ", "backspace", "delete":
		return true
	}
	return false
}

// deleteKey arms p, or deletes it when confirm is set and p is armed.
func (m *Model) deleteKey(p *model.Post, confirm bool) tea.Cmd {
	if p == nil {
		return nil
	}
	key := p.Id
	if p.Id == "" {
		// a message that never reached the server is only ours to discard
		if !m.failed[p.PendingPostId] {
			return nil
		}
		key = p.PendingPostId
	} else if p.UserId != m.c.Me.Id {
		m.setStatus(tr("You can only delete your own messages"), true)
		return nil
	}
	if m.delArm != key {
		m.armDelete(key, p)
		return nil
	}
	if !confirm {
		return nil
	}
	m.delArm, m.delAsk = "", ""
	if p.Id == "" {
		delete(m.failed, p.PendingPostId)
		m.dropLocal(p)
		m.setStatus(tr("Discarded the unsent message"), false)
		return nil
	}
	gone := m.dropLocal(p)
	m.setStatus(tr("Message deleted"), false)
	id := p.Id
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		return deletedMsg{posts: gone, err: m.c.DeletePost(cx, id)}
	}
}

func (m *Model) armDelete(key string, p *model.Post) {
	m.delArm = key
	q := tr("Delete this message? Enter deletes, any other key cancels")
	if n := m.replyCount(p); n > 0 {
		q = fmt.Sprintf(tr("Delete this message and its %d replies? Enter deletes, any other key cancels"), n)
	}
	m.delAsk = q
	m.refresh(false)
}

// replyCount is the server's count, or the replies on screen when more
// arrived since.
func (m *Model) replyCount(p *model.Post) int {
	if p.Id == "" || p.RootId != "" {
		return 0
	}
	n := 0
	if m.thread != nil && m.thread.root == p.Id {
		n = len(m.thread.posts) - 1
	} else if b := m.bufs[p.ChannelId]; b != nil {
		for _, q := range b.posts {
			if q.RootId == p.Id {
				n++
			}
		}
	}
	return max(n, int(p.ReplyCount))
}

// armed reports whether p is waiting for the second d.
func (m *Model) armed(p *model.Post) bool {
	return m.delArm != "" && (p.Id == m.delArm || (p.Id == "" && p.PendingPostId == m.delArm))
}

// disarm cancels a pending delete; it reports whether one was pending.
func (m *Model) disarm() bool {
	if m.delArm == "" {
		return false
	}
	m.delArm, m.delAsk = "", ""
	m.refresh(false)
	return true
}

// dropLocal takes p off screen right away and returns what it took. Deleting
// a root takes its replies too, as the server deletes them with it, and
// closes the thread if it is open.
func (m *Model) dropLocal(p *model.Post) []*model.Post {
	var gone []*model.Post
	if b := m.bufs[p.ChannelId]; b != nil {
		if p.Id == "" {
			b.posts = removePending(b.posts, p.PendingPostId)
		} else {
			b.posts, gone = removeWithReplies(b.posts, p.Id)
		}
	}
	if m.thread != nil {
		switch {
		case p.Id == "":
			m.thread.posts = removePending(m.thread.posts, p.PendingPostId)
		case p.Id == m.thread.root:
			m.closeThread()
		default:
			m.thread.posts = remove(m.thread.posts, p.Id)
		}
	}
	if pn := m.p(); len(pn.visible) <= 1 {
		m.focus = focusInput
		m.input.Focus()
	}
	m.refresh(false)
	if len(gone) == 0 {
		gone = []*model.Post{p}
	}
	return gone
}

// onDeleted puts the messages back when the server would not delete them.
func (m *Model) onDeleted(msg deletedMsg) {
	if msg.err == nil {
		return
	}
	for _, p := range msg.posts {
		if b := m.bufs[p.ChannelId]; b != nil {
			b.posts = upsert(b.posts, p)
		}
		if m.thread != nil && (p.RootId == m.thread.root || p.Id == m.thread.root) {
			m.thread.posts = upsert(m.thread.posts, p)
		}
	}
	m.setStatus(tr("Cannot delete: ")+msg.err.Error(), true)
	m.refresh(false)
}

// removeWithReplies drops the post id and every reply to it.
func removeWithReplies(list []*model.Post, id string) (kept, gone []*model.Post) {
	kept = list[:0]
	for _, q := range list {
		if q.Id == id || q.RootId == id {
			gone = append(gone, q)
		} else {
			kept = append(kept, q)
		}
	}
	return kept, gone
}

func removePending(list []*model.Post, pending string) []*model.Post {
	for i, q := range list {
		if q.Id == "" && q.PendingPostId == pending {
			return append(list[:i], list[i+1:]...)
		}
	}
	return list
}
