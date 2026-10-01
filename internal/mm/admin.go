package mm

import (
	"context"
	"fmt"
	"strings"

	"github.com/mattermost/mattermost/server/public/model"
)

// Management calls used by the in-app commands (/channel, /bot, /token, …).
// Permissions are enforced by the server; its error is returned as is.

func (c *Client) CreateChannel(ctx context.Context, teamID, name, display, purpose string, private bool) (*model.Channel, error) {
	typ := model.ChannelTypeOpen
	if private {
		typ = model.ChannelTypePrivate
	}
	ch, _, err := c.API.CreateChannel(ctx, &model.Channel{TeamId: teamID, Name: name, DisplayName: display, Purpose: purpose, Type: typ})
	return CleanChannel(ch), err
}

func (c *Client) RenameChannel(ctx context.Context, channelID, display string) (*model.Channel, error) {
	ch, _, err := c.API.PatchChannel(ctx, channelID, &model.ChannelPatch{DisplayName: &display})
	return CleanChannel(ch), err
}

func (c *Client) ArchiveChannel(ctx context.Context, channelID string) error {
	_, err := c.API.DeleteChannel(ctx, channelID)
	return err
}

// ChannelMembers returns up to 200 members of a channel.
func (c *Client) ChannelMembers(ctx context.Context, channelID string) ([]*model.User, error) {
	members, _, err := c.API.GetChannelMembers(ctx, channelID, 0, 200, "")
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(members))
	for i, mem := range members {
		ids[i] = mem.UserId
	}
	if err := c.EnsureUsers(ctx, ids); err != nil {
		return nil, err
	}
	users := make([]*model.User, 0, len(ids))
	for _, id := range ids {
		if u := c.User(id); u != nil {
			users = append(users, u)
		}
	}
	return users, nil
}

// UsersByNames resolves usernames (with or without @).
func (c *Client) UsersByNames(ctx context.Context, names []string) ([]*model.User, error) {
	var out []*model.User
	for _, n := range names {
		n = strings.TrimPrefix(strings.TrimSpace(n), "@")
		if n == "" {
			continue
		}
		u, _, err := c.API.GetUserByUsername(ctx, n, "")
		if err != nil {
			return nil, fmt.Errorf("@%s: %w", n, err)
		}
		c.addUsers([]*model.User{u})
		out = append(out, u)
	}
	return out, nil
}

func (c *Client) AddToChannel(ctx context.Context, channelID, userID string) error {
	_, _, err := c.API.AddChannelMember(ctx, channelID, userID)
	return err
}

func (c *Client) RemoveFromChannel(ctx context.Context, channelID, userID string) error {
	_, err := c.API.RemoveUserFromChannel(ctx, channelID, userID)
	return err
}

func (c *Client) AddToTeam(ctx context.Context, teamID, userID string) error {
	_, _, err := c.API.AddTeamMember(ctx, teamID, userID)
	return err
}

// GroupDM opens a group message with the given users and the current user.
func (c *Client) GroupDM(ctx context.Context, userIDs []string) (*model.Channel, error) {
	ch, _, err := c.API.CreateGroupChannel(ctx, append([]string{c.Me.Id}, userIDs...))
	return CleanChannel(ch), err
}

func (c *Client) CreateBot(ctx context.Context, username, display, description string) (*model.Bot, error) {
	bot, _, err := c.API.CreateBot(ctx, &model.Bot{Username: username, DisplayName: display, Description: description})
	return bot, err
}

func (c *Client) Bots(ctx context.Context) ([]*model.Bot, error) {
	bots, _, err := c.API.GetBots(ctx, 0, 200, "")
	for _, b := range bots {
		b.Username, b.DisplayName, b.Description = Clean(b.Username), Clean(b.DisplayName), Clean(b.Description)
	}
	return bots, err
}

func (c *Client) CreateToken(ctx context.Context, userID, description string) (*model.UserAccessToken, error) {
	t, _, err := c.API.CreateUserAccessToken(ctx, userID, description, 0)
	return t, err
}

// Tokens lists the current user's personal access tokens.
func (c *Client) Tokens(ctx context.Context) ([]*model.UserAccessToken, error) {
	ts, _, err := c.API.GetUserAccessTokensForUser(ctx, c.Me.Id, 0, 200)
	for _, t := range ts {
		t.Description = Clean(t.Description)
	}
	return ts, err
}

func (c *Client) RevokeToken(ctx context.Context, id string) error {
	_, err := c.API.RevokeUserAccessToken(ctx, id)
	return err
}

// UserInfo returns a user and their status ("online", "away", …).
func (c *Client) UserInfo(ctx context.Context, username string) (*model.User, string, error) {
	us, err := c.UsersByNames(ctx, []string{username})
	if err != nil || len(us) == 0 {
		return nil, "", err
	}
	st, _, err := c.API.GetUserStatus(ctx, us[0].Id, "")
	if err != nil {
		return us[0], "", nil
	}
	return us[0], st.Status, nil
}
