// Package background is mmt's notification agent: a small process that
// stays connected after every mmt window is closed and shows a desktop
// notification for mentions, direct messages and incoming calls. Clicking
// one opens mmt on that channel.
package background

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/codihaus/mmt/internal/config"
	"github.com/codihaus/mmt/internal/i18n"
	"github.com/codihaus/mmt/internal/launcher"
	"github.com/codihaus/mmt/internal/mm"
)

const (
	callPostType  = "custom_calls"
	callStartType = "custom_com.mattermost.calls_call_start"
)

type agent struct {
	exe  string // the mmt binary to open on click
	lock bool   // the app lock is on: notifications say nothing specific

	c     *mm.Client
	mu    sync.Mutex
	muted map[string]bool
	types map[string]model.ChannelType
}

// Run is the agent's main; it must be called on the main thread. exe is the
// mmt binary a click should open.
func Run(exe string) error {
	if _, err := os.Stat(exe); err != nil {
		exe, _ = os.Executable()
	}
	a := &agent{exe: exe}
	go a.loop()
	runMain(a.open)
	return nil
}

// loop waits for a login, connects and listens until the process ends.
func (a *agent) loop() {
	for wait := time.Duration(0); ; wait = time.Minute {
		time.Sleep(wait)
		cfg, err := config.Load()
		if err != nil || cfg.ServerURL == "" {
			continue
		}
		token, err := config.Token(cfg.ServerURL)
		if err != nil || token == "" {
			continue
		}
		i18n.Set(i18n.Detect(cfg.Language))
		a.lock = cfg.Lock != ""
		c := mm.New(cfg.ServerURL, token)
		cx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		err = c.Connect(cx)
		cancel()
		if err != nil {
			log.Printf("connect %s: %v", cfg.ServerURL, err)
			continue
		}
		a.c = c
		log.Printf("connected to %s as @%s", cfg.ServerURL, c.Me.Username)
		c.Listen(context.Background(), a.handle, func(ok bool, err error) {
			if ok {
				go a.loadChannels()
			} else if err != nil {
				log.Printf("websocket: %v", err)
			}
		})
	}
}

// loadChannels refreshes which channels are DMs and which are muted.
func (a *agent) loadChannels() {
	cx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	s, err := a.c.Bootstrap(cx)
	if err != nil {
		log.Printf("channels: %v", err)
		return
	}
	muted := map[string]bool{}
	types := map[string]model.ChannelType{}
	for _, ch := range s.Channels {
		types[ch.Id] = ch.Type
		if m := s.Members[ch.Id]; m != nil {
			muted[ch.Id] = isMuted(m)
		}
	}
	a.mu.Lock()
	a.muted, a.types = muted, types
	a.mu.Unlock()
}

func isMuted(m *model.ChannelMember) bool {
	return m.NotifyProps[model.MarkUnreadNotifyProp] == model.ChannelMarkUnreadMention
}

func (a *agent) channelType(id string) model.ChannelType {
	a.mu.Lock()
	t, ok := a.types[id]
	a.mu.Unlock()
	if ok {
		return t
	}
	cx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ch, err := a.c.Channel(cx, id)
	if err != nil || ch == nil {
		return ""
	}
	a.mu.Lock()
	if a.types == nil {
		a.types = map[string]model.ChannelType{}
	}
	a.types[id] = ch.Type
	a.mu.Unlock()
	return ch.Type
}

func (a *agent) handle(ev *model.WebSocketEvent) {
	data := ev.GetData()
	switch string(ev.EventType()) {
	case string(model.WebsocketEventPosted):
		a.onPosted(data)
	case string(model.WebsocketEventChannelMemberUpdated):
		raw, _ := data["channelMember"].(string)
		var m model.ChannelMember
		if raw != "" && json.Unmarshal([]byte(raw), &m) == nil && m.UserId == a.c.Me.Id {
			a.mu.Lock()
			if a.muted == nil {
				a.muted = map[string]bool{}
			}
			a.muted[m.ChannelId] = isMuted(&m)
			a.mu.Unlock()
		}
	case callStartType:
		ch, _ := data["channelID"].(string)
		if ch == "" {
			ch = ev.GetBroadcast().ChannelId
		}
		owner, _ := data["owner_id"].(string)
		if ch == "" || owner == a.c.Me.Id || !isDM(a.channelType(ch)) {
			return
		}
		name := owner
		cx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := a.c.EnsureUsers(cx, []string{owner}); err == nil {
			name = a.c.Username(owner)
		}
		cancel()
		a.notify(i18n.T("Incoming call"), fmt.Sprintf(i18n.T("%s is calling you"), "@"+name), ch)
	}
}

func isDM(t model.ChannelType) bool {
	return t == model.ChannelTypeDirect || t == model.ChannelTypeGroup
}

func (a *agent) onPosted(data map[string]any) {
	raw, _ := data["post"].(string)
	var p model.Post
	if raw == "" || json.Unmarshal([]byte(raw), &p) != nil {
		return
	}
	mm.CleanPost(&p)
	// call posts are announced by the call_start event instead
	if p.UserId == a.c.Me.Id || strings.HasPrefix(p.Type, "system_") || p.Type == callPostType {
		return
	}
	mentioned := false
	if raw, _ := data["mentions"].(string); raw != "" {
		var ids []string
		if json.Unmarshal([]byte(raw), &ids) == nil {
			for _, id := range ids {
				mentioned = mentioned || id == a.c.Me.Id
			}
		}
	}
	a.mu.Lock()
	muted := a.muted[p.ChannelId]
	a.mu.Unlock()
	dm := isDM(model.ChannelType(str(data, "channel_type"))) && !muted
	if !mentioned && !dm {
		return
	}
	title := mm.Clean(str(data, "sender_name"))
	if !dm {
		title += i18n.T(" in ") + mm.Clean(str(data, "channel_display_name"))
	}
	body := strings.TrimSpace(p.Message)
	if body == "" && len(p.FileIds) > 0 {
		body = i18n.T("sent a file")
	}
	a.notify(title, body, p.ChannelId)
}

func str(data map[string]any, key string) string {
	s, _ := data[key].(string)
	return s
}

func (a *agent) notify(title, body, channel string) {
	// an open mmt window shows its own notifications
	if uiOpen() {
		return
	}
	if a.lock {
		title, body = "mmt", i18n.T("New message")
	}
	if r := []rune(body); len(r) > 200 {
		body = string(r[:200]) + "…"
	}
	show(title, body, channel)
}

// open is called when a notification is clicked: it opens mmt on that
// channel in the configured terminal.
func (a *agent) open(channel string) {
	cfg, err := config.Load()
	if err != nil {
		log.Printf("open: %v", err)
		return
	}
	if channel != "" {
		cfg.LastChannel = channel
		if err := cfg.Save(); err != nil {
			log.Printf("open: %v", err)
		}
	}
	target := launcher.Resolve(launcher.Kind(cfg.Terminal))
	if target == launcher.Current {
		err = exec.Command("open", "-a", "Terminal", a.exe).Run()
	} else {
		err = launcher.Launch(target, a.exe)
	}
	if err != nil {
		log.Printf("open mmt: %v", err)
	}
}
