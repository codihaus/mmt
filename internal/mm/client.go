// Package mm wraps the official Mattermost Go client with the small surface
// the TUI needs, plus a user cache and a self-reconnecting WebSocket loop.
package mm

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
)

const PageSize = 60

type Client struct {
	API    *model.Client4
	Me     *model.User
	server string
	token  string

	mu    sync.RWMutex
	users map[string]*model.User

	wsMu sync.Mutex
	ws   *model.WebSocketClient
}

func New(server, token string) *Client {
	api := model.NewAPIv4Client(server)
	api.SetToken(token)
	return &Client{API: api, server: server, token: token, users: map[string]*model.User{}}
}

func (c *Client) Server() string { return c.server }

// CallURL opens the Calls plugin's call window for a channel, which joins
// the running call. Any team the user is in works for a DM.
func CallURL(server, team, channelID string) string {
	return server + "/" + team + "/com.mattermost.calls/expanded/" + channelID
}

// Login exchanges username/password (and optional MFA code) for a session token.
func Login(ctx context.Context, server, login, password, mfa string) (string, error) {
	api := model.NewAPIv4Client(server)
	if _, _, err := api.LoginWithMFA(ctx, login, password, mfa); err != nil {
		return "", err
	}
	return api.AuthToken, nil
}

func (c *Client) Connect(ctx context.Context) error {
	me, _, err := c.API.GetMe(ctx, "")
	if err != nil {
		return err
	}
	c.Me = CleanUser(me)
	c.addUsers([]*model.User{me})
	return nil
}

func (c *Client) addUsers(us []*model.User) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, u := range us {
		c.users[u.Id] = CleanUser(u)
	}
}

func (c *Client) User(id string) *model.User {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.users[id]
}

func (c *Client) Username(id string) string {
	if u := c.User(id); u != nil {
		return u.Username
	}
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// Missing returns the ids not yet in the user cache.
func (c *Client) Missing(ids []string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	seen := map[string]bool{}
	var out []string
	for _, id := range ids {
		if id == "" || seen[id] || c.users[id] != nil {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func (c *Client) EnsureUsers(ctx context.Context, ids []string) error {
	missing := c.Missing(ids)
	if len(missing) == 0 {
		return nil
	}
	us, _, err := c.API.GetUsersByIds(ctx, missing)
	if err != nil {
		return err
	}
	c.addUsers(us)
	return nil
}

// DMPartner returns the other user's id of a direct channel.
func (c *Client) DMPartner(ch *model.Channel) string {
	a, b, ok := strings.Cut(ch.Name, "__")
	if !ok {
		return ""
	}
	if a == c.Me.Id {
		return b
	}
	return a
}

type Snapshot struct {
	Teams    []*model.Team
	Channels []*model.Channel
	Members  map[string]*model.ChannelMember
	// Categories holds the user's sidebar layout per team. A team is absent
	// when the server does not support categories.
	Categories map[string]*model.OrderedSidebarCategories
}

// Bootstrap loads teams, all channels of the user (deduplicated, DMs appear in
// every team) and channel memberships for unread counts.
func (c *Client) Bootstrap(ctx context.Context) (*Snapshot, error) {
	teams, _, err := c.API.GetTeamsForUser(ctx, c.Me.Id, "")
	if err != nil {
		return nil, err
	}
	for _, t := range teams {
		cleanTeam(t)
	}
	s := &Snapshot{
		Teams:      teams,
		Members:    map[string]*model.ChannelMember{},
		Categories: map[string]*model.OrderedSidebarCategories{},
	}
	seen := map[string]bool{}
	var partners []string
	for _, t := range teams {
		chs, _, err := c.API.GetChannelsForTeamForUser(ctx, t.Id, c.Me.Id, false, "")
		if err != nil {
			return nil, err
		}
		for _, ch := range chs {
			if seen[ch.Id] || ch.DeleteAt > 0 {
				continue
			}
			seen[ch.Id] = true
			s.Channels = append(s.Channels, CleanChannel(ch))
			if ch.Type == model.ChannelTypeDirect {
				partners = append(partners, c.DMPartner(ch))
			}
		}
		members, _, err := c.API.GetChannelMembersForUser(ctx, c.Me.Id, t.Id, "")
		if err != nil {
			return nil, err
		}
		for i := range members {
			s.Members[members[i].ChannelId] = &members[i]
		}
		if cats, err := c.Categories(ctx, t.Id); err == nil {
			s.Categories[t.Id] = cats
		}
	}
	if err := c.EnsureUsers(ctx, partners); err != nil {
		return nil, err
	}
	return s, nil
}

func (c *Client) Categories(ctx context.Context, teamID string) (*model.OrderedSidebarCategories, error) {
	cats, _, err := c.API.GetSidebarCategoriesForTeamForUser(ctx, c.Me.Id, teamID, "")
	if err != nil {
		return nil, err
	}
	for _, cat := range cats.Categories {
		cat.DisplayName = Clean(cat.DisplayName)
	}
	return cats, nil
}

func sortPosts(pl *model.PostList) []*model.Post {
	out := make([]*model.Post, 0, len(pl.Posts))
	for _, p := range pl.Posts {
		out = append(out, CleanPost(p))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreateAt < out[j].CreateAt })
	return out
}

// Posts returns the newest page of channel history, or the page before post
// `before` when it is set, oldest first, and whether older posts may exist.
// Paging by post id stays correct while others post or delete messages.
func (c *Client) Posts(ctx context.Context, channelID, before string) ([]*model.Post, bool, error) {
	var pl *model.PostList
	var err error
	if before == "" {
		pl, _, err = c.API.GetPostsForChannel(ctx, channelID, 0, PageSize, "", false, false)
	} else {
		pl, _, err = c.API.GetPostsBefore(ctx, channelID, before, 0, PageSize, "", false, false)
	}
	if err != nil {
		return nil, false, err
	}
	return sortPosts(pl), len(pl.Order) == PageSize, nil
}

func (c *Client) Thread(ctx context.Context, rootID string) ([]*model.Post, error) {
	pl, _, err := c.API.GetPostThread(ctx, rootID, "", false)
	if err != nil {
		return nil, err
	}
	return sortPosts(pl), nil
}

func (c *Client) Send(ctx context.Context, p *model.Post) (*model.Post, error) {
	created, _, err := c.API.CreatePost(ctx, p)
	return CleanPost(created), err
}

func (c *Client) Upload(ctx context.Context, channelID, name string, data []byte) (string, error) {
	res, _, err := c.API.UploadFile(ctx, data, channelID, name)
	if err != nil {
		return "", err
	}
	if len(res.FileInfos) == 0 {
		return "", errors.New("upload returned no file")
	}
	return res.FileInfos[0].Id, nil
}

func (c *Client) File(ctx context.Context, id string) ([]byte, error) {
	data, _, err := c.API.GetFile(ctx, id)
	return data, err
}

// React adds the emoji to a post as the current user; on is false to take
// it back.
func (c *Client) React(ctx context.Context, postID, emoji string, on bool) error {
	r := &model.Reaction{UserId: c.Me.Id, PostId: postID, EmojiName: emoji}
	if on {
		_, _, err := c.API.SaveReaction(ctx, r)
		return err
	}
	_, err := c.API.DeleteReaction(ctx, r)
	return err
}

func (c *Client) DeletePost(ctx context.Context, postID string) error {
	_, err := c.API.DeletePost(ctx, postID)
	return err
}

func (c *Client) View(ctx context.Context, channelID, prevID string) error {
	_, _, err := c.API.ViewChannel(ctx, c.Me.Id, &model.ChannelView{ChannelId: channelID, PrevChannelId: prevID})
	return err
}

func (c *Client) Channel(ctx context.Context, id string) (*model.Channel, error) {
	ch, _, err := c.API.GetChannel(ctx, id)
	return CleanChannel(ch), err
}

func (c *Client) OpenDM(ctx context.Context, username string) (*model.Channel, error) {
	u, _, err := c.API.GetUserByUsername(ctx, strings.TrimPrefix(username, "@"), "")
	if err != nil {
		return nil, err
	}
	c.addUsers([]*model.User{u})
	ch, _, err := c.API.CreateDirectChannel(ctx, c.Me.Id, u.Id)
	return CleanChannel(ch), err
}

func (c *Client) Autocomplete(ctx context.Context, term string) ([]*model.User, error) {
	res, _, err := c.API.AutocompleteUsers(ctx, term, 8, "")
	if err != nil {
		return nil, err
	}
	c.addUsers(res.Users)
	return res.Users, nil
}

// AutocompleteInChannel suggests only members of the channel, so a message
// cannot mention someone who will never see it.
func (c *Client) AutocompleteInChannel(ctx context.Context, teamID, channelID, term string) ([]*model.User, error) {
	res, _, err := c.API.AutocompleteUsersInChannel(ctx, teamID, channelID, term, 8, "")
	if err != nil {
		return nil, err
	}
	c.addUsers(res.Users)
	return res.Users, nil
}

func (c *Client) Command(ctx context.Context, channelID, cmd string) error {
	_, _, err := c.API.ExecuteCommand(ctx, channelID, cmd)
	return err
}

// Typing sends a typing indicator when the WebSocket is connected.
func (c *Client) Typing(channelID, parentID string) {
	c.wsMu.Lock()
	ws := c.ws
	c.wsMu.Unlock()
	if ws == nil {
		return
	}
	// the socket can close between the check and the send, and the client
	// library panics on a send to its closed write channel
	defer func() { _ = recover() }()
	ws.UserTyping(channelID, parentID)
}

// SearchChannels returns public channels of a team matching term.
func (c *Client) SearchChannels(ctx context.Context, teamID, term string) ([]*model.Channel, error) {
	list, _, err := c.API.AutocompleteChannelsForTeam(ctx, teamID, term)
	if err != nil {
		return nil, err
	}
	for _, ch := range list {
		CleanChannel(ch)
	}
	return list, nil
}

func (c *Client) Join(ctx context.Context, channelID string) error {
	_, _, err := c.API.AddChannelMember(ctx, channelID, c.Me.Id)
	return err
}

// Logout ends the server session behind the token (a no-op for personal
// access tokens, which stay valid until revoked in the web app).
func (c *Client) Logout(ctx context.Context) error {
	_, err := c.API.Logout(ctx)
	return err
}

func (c *Client) setWS(ws *model.WebSocketClient) {
	c.wsMu.Lock()
	c.ws = ws
	c.wsMu.Unlock()
}

// Listen keeps a WebSocket connection open until ctx is done, reconnecting
// with exponential backoff. Callbacks run on the listener goroutine.
func (c *Client) Listen(ctx context.Context, onEvent func(*model.WebSocketEvent), onState func(connected bool, err error)) {
	wsURL := "ws" + strings.TrimPrefix(c.server, "http")
	backoff := time.Second
	sleep := func() bool {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
		return true
	}
	for ctx.Err() == nil {
		ws, err := model.NewWebSocketClient4(wsURL, c.token)
		if err != nil {
			onState(false, err)
			if !sleep() {
				return
			}
			continue
		}
		ws.Listen()
		c.setWS(ws)
		onState(true, nil)
		backoff = time.Second
		go func() {
			for range ws.ResponseChannel {
			}
		}()
	loop:
		for {
			select {
			case ev, ok := <-ws.EventChannel:
				if !ok {
					break loop
				}
				onEvent(ev)
			case <-ws.PingTimeoutChannel:
				c.setWS(nil)
				ws.Close()
			case <-ctx.Done():
				c.setWS(nil)
				ws.Close()
				return
			}
		}
		c.setWS(nil)
		var lerr error
		if ws.ListenError != nil {
			lerr = ws.ListenError
		}
		onState(false, lerr)
		if !sleep() {
			return
		}
	}
}
