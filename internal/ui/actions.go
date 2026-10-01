package ui

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattermost/mattermost/server/public/model"
)

// actions are the management commands. Titles, help and labels are English
// source strings, translated when shown.
var actions = []action{
	{
		cmd: "/channel new", title: "Create a channel", help: "A new channel in the current team. You are added and switched to it.",
		fields: []field{
			{key: "name", label: "Name", kind: fText, hint: "Shown in the sidebar, e.g. Marketing Team"},
			{key: "private", label: "Private?", kind: fBool, def: "no", hint: "Private channels are invite-only. y / n"},
			{key: "purpose", label: "Purpose", kind: fText, optional: true, hint: "One line on what the channel is for"},
		},
		run: runChannelNew,
	},
	{
		cmd: "/channel rename", title: "Rename this channel", help: "Changes the display name of the current channel.",
		fields: []field{{key: "name", label: "New name", kind: fText}},
		run:    runChannelRename,
	},
	{cmd: "/channel info", title: "Channel info", help: "Details and members of the current channel.", run: runChannelInfo},
	{
		cmd: "/channel archive", title: "Archive this channel", help: "Archives the current channel for everyone. Admins can restore it.",
		confirm: true, run: runChannelArchive,
	},
	{
		cmd: "/add", title: "Add people to this channel", help: "They must already be in the team (see /team add).",
		fields: []field{{key: "users", label: "People", kind: fUsers, hint: "Type @ and a name; Tab completes. Several are fine."}},
		run:    runAdd,
	},
	{
		cmd: "/remove", title: "Remove people from this channel", help: "They can rejoin public channels by themselves.",
		fields:  []field{{key: "users", label: "People", kind: fUsers, hint: "Type @ and a name; Tab completes."}},
		confirm: true, run: runRemove,
	},
	{
		cmd: "/dm", title: "Message people", help: "One person opens a direct message, several open a group message.",
		fields: []field{{key: "users", label: "People", kind: fUsers, hint: "Type @ and a name; Tab completes. Up to 7 people."}},
		run:    runDM,
	},
	{
		cmd: "/team add", title: "Add people to this team", help: "Adds existing accounts to the current team.",
		fields: []field{{key: "users", label: "People", kind: fUsers, hint: "Type @ and a name; Tab completes."}},
		run:    runTeamAdd,
	},
	{
		cmd: "/user", title: "User profile", help: "Name, status and details of a person.",
		fields: []field{{key: "user", label: "Person", kind: fUser, hint: "Type @ and a name; Tab completes."}},
		run:    runUserInfo,
	},
	{
		cmd: "/bot new", title: "Create a bot", help: "Bots post through integrations with their own token. Needs the permission to manage bots.",
		fields: []field{
			{key: "username", label: "Username", kind: fWord, hint: "Lowercase, e.g. deploy-bot"},
			{key: "display", label: "Display name", kind: fText, optional: true},
			{key: "description", label: "Description", kind: fText, optional: true},
			{key: "token", label: "Create a token?", kind: fBool, def: "yes", hint: "The token is shown once and copied to the clipboard. y / n"},
		},
		run: runBotNew,
	},
	{cmd: "/bot list", title: "Bots", help: "Bots on this server.", run: runBotList},
	{
		cmd: "/bot token", title: "Create a bot token", help: "A new access token for a bot. It is shown once and copied to the clipboard.",
		fields: []field{
			{key: "bot", label: "Bot", kind: fOption, hint: "Type to filter; Tab completes."},
			{key: "description", label: "Description", kind: fText, def: "created with mmt"},
		},
		load: loadBots, run: runBotToken,
	},
	{
		cmd: "/bot add", title: "Add a bot to a channel", help: "Adds the bot to the current team and to the channel.",
		fields: []field{
			{key: "bot", label: "Bot", kind: fOption, hint: "Type to filter; Tab completes."},
			{key: "channel", label: "Channel", kind: fChannel, optional: true, hint: "Empty for the current channel. Type ~ and a name."},
		},
		load: loadBots, run: runBotAdd,
	},
	{
		cmd: "/token new", title: "Create a personal access token", help: "Lets scripts act as you. It is shown once and copied to the clipboard.",
		fields: []field{{key: "description", label: "Description", kind: fText, hint: "What it is for, e.g. CI deploys"}},
		run:    runTokenNew,
	},
	{cmd: "/token list", title: "Your access tokens", help: "Personal access tokens of your account.", run: runTokenList},
	{
		cmd: "/token revoke", title: "Revoke a token", help: "Scripts using the token stop working at once.",
		fields:  []field{{key: "token", label: "Token", kind: fOption, hint: "Type to filter; Tab completes."}},
		confirm: true, load: loadTokens, run: runTokenRevoke,
	},
}

func done(title string, lines ...string) actionDoneMsg {
	return actionDoneMsg{title: title, lines: lines}
}

// slugRe keeps what Mattermost allows in a channel URL name.
var slugRe = regexp.MustCompile(`[^a-z0-9_-]+`)

// channelSlug derives the URL name from a display name: "Cơ hội Dự Án" ->
// "co-hoi-du-an".
func channelSlug(display string) string {
	s := strings.Trim(slugRe.ReplaceAllString(fold(display), "-"), "-_")
	if len(s) < 2 {
		s = "channel-" + model.NewId()[:6]
	}
	return strings.TrimRight(s[:min(len(s), 64)], "-_")
}

func runChannelNew(m *Model, v wizVals) tea.Cmd {
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		ch, err := m.c.CreateChannel(cx, v["_team"], channelSlug(v["name"]), v["name"], v["purpose"], v["private"] == "yes")
		if err != nil {
			return actionDoneMsg{err: err}
		}
		return actionDoneMsg{open: ch}
	}
}

func runChannelRename(m *Model, v wizVals) tea.Cmd {
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		if _, err := m.c.RenameChannel(cx, v["_channel"], v["name"]); err != nil {
			return actionDoneMsg{err: err}
		}
		return done(tr("Rename this channel"), fmt.Sprintf(tr("Renamed to %s."), v["name"]))
	}
}

func runChannelInfo(m *Model, v wizVals) tea.Cmd {
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		ch, err := m.c.Channel(cx, v["_channel"])
		if err != nil {
			return actionDoneMsg{err: err}
		}
		users, err := m.c.ChannelMembers(cx, ch.Id)
		if err != nil {
			return actionDoneMsg{err: err}
		}
		kind := map[model.ChannelType]string{model.ChannelTypeOpen: tr("public"), model.ChannelTypePrivate: tr("private"),
			model.ChannelTypeDirect: tr("direct message"), model.ChannelTypeGroup: tr("group message")}[ch.Type]
		lines := []string{
			tr("Name: ") + ch.DisplayName + "  (~" + ch.Name + ", " + kind + ")",
			tr("Created: ") + fmtTime(ch.CreateAt),
		}
		if ch.Purpose != "" {
			lines = append(lines, tr("Purpose: ")+ch.Purpose)
		}
		if ch.Header != "" {
			lines = append(lines, tr("Header: ")+ch.Header)
		}
		names := make([]string, len(users))
		for i, u := range users {
			names[i] = "@" + u.Username
		}
		lines = append(lines, "", fmt.Sprintf(tr("%d members: "), len(users))+strings.Join(names, " "))
		return done(v["_channelLabel"], lines...)
	}
}

func runChannelArchive(m *Model, v wizVals) tea.Cmd {
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		if err := m.c.ArchiveChannel(cx, v["_channel"]); err != nil {
			return actionDoneMsg{err: err}
		}
		return actionDoneMsg{removed: v["_channel"], title: tr("Archive this channel"), lines: []string{v["_channelLabel"] + " " + tr("was archived.")}}
	}
}

// eachUser runs fn for every @user in v and reports per-user results.
func eachUser(m *Model, v wizVals, title, okText string, fn func(u *model.User) error) tea.Cmd {
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		users, err := m.c.UsersByNames(cx, v.users())
		if err != nil {
			return actionDoneMsg{err: err}
		}
		var lines []string
		for _, u := range users {
			if err := fn(u); err != nil {
				lines = append(lines, "✗ @"+u.Username+": "+err.Error())
			} else {
				lines = append(lines, "✓ @"+u.Username+" "+okText)
			}
		}
		return done(title, lines...)
	}
}

func runAdd(m *Model, v wizVals) tea.Cmd {
	return eachUser(m, v, tr("Add people to this channel"), tr("added"), func(u *model.User) error {
		cx, cancel := ctx()
		defer cancel()
		return m.c.AddToChannel(cx, v["_channel"], u.Id)
	})
}

func runRemove(m *Model, v wizVals) tea.Cmd {
	return eachUser(m, v, tr("Remove people from this channel"), tr("removed"), func(u *model.User) error {
		cx, cancel := ctx()
		defer cancel()
		return m.c.RemoveFromChannel(cx, v["_channel"], u.Id)
	})
}

func runTeamAdd(m *Model, v wizVals) tea.Cmd {
	return eachUser(m, v, tr("Add people to this team"), tr("added"), func(u *model.User) error {
		cx, cancel := ctx()
		defer cancel()
		return m.c.AddToTeam(cx, v["_team"], u.Id)
	})
}

func runDM(m *Model, v wizVals) tea.Cmd {
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		users, err := m.c.UsersByNames(cx, v.users())
		if err != nil {
			return actionDoneMsg{err: err}
		}
		var ch *model.Channel
		switch len(users) {
		case 0:
			return actionDoneMsg{err: errors.New(tr("Usage: /dm @user"))}
		case 1:
			ch, err = m.c.OpenDM(cx, users[0].Username)
		default:
			ids := make([]string, len(users))
			for i, u := range users {
				ids[i] = u.Id
			}
			ch, err = m.c.GroupDM(cx, ids)
		}
		if err != nil {
			return actionDoneMsg{err: err}
		}
		if ch.Type == model.ChannelTypeDirect {
			_ = m.c.EnsureUsers(cx, []string{m.c.DMPartner(ch)})
		}
		return actionDoneMsg{open: ch}
	}
}

func runUserInfo(m *Model, v wizVals) tea.Cmd {
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		u, status, err := m.c.UserInfo(cx, v["user"])
		if err != nil {
			return actionDoneMsg{err: err}
		}
		lines := []string{"@" + u.Username + "  " + fullName(u)}
		if u.Nickname != "" {
			lines = append(lines, tr("Nickname: ")+u.Nickname)
		}
		if u.Position != "" {
			lines = append(lines, tr("Position: ")+u.Position)
		}
		if u.Email != "" {
			lines = append(lines, "Email: "+u.Email)
		}
		if status != "" {
			lines = append(lines, tr("Status: ")+status)
		}
		if u.IsBot {
			lines = append(lines, tr("This is a bot account."))
		}
		if u.Roles != "" {
			lines = append(lines, tr("Roles: ")+u.Roles)
		}
		return done(tr("User profile"), lines...)
	}
}

func tokenLines(t *model.UserAccessToken) []string {
	return []string{
		tr("Token (shown once, copied to the clipboard):"),
		"",
		t.Token,
		"",
		tr("Token ID (for revoking): ") + t.Id,
	}
}

func runBotNew(m *Model, v wizVals) tea.Cmd {
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		bot, err := m.c.CreateBot(cx, strings.ToLower(strings.TrimPrefix(v["username"], "@")), v["display"], v["description"])
		if err != nil {
			return actionDoneMsg{err: err}
		}
		lines := []string{fmt.Sprintf(tr("Bot @%s created."), bot.Username)}
		if v["token"] != "yes" {
			return done(tr("Create a bot"), lines...)
		}
		t, err := m.c.CreateToken(cx, bot.UserId, "created with mmt")
		if err != nil {
			return done(tr("Create a bot"), append(lines, tr("Token not created: ")+err.Error())...)
		}
		return actionDoneMsg{title: tr("Create a bot"), lines: append(append(lines, ""), tokenLines(t)...), copy: t.Token}
	}
}

func runBotList(m *Model, v wizVals) tea.Cmd {
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		bots, err := m.c.Bots(cx)
		if err != nil {
			return actionDoneMsg{err: err}
		}
		if len(bots) == 0 {
			return done(tr("Bots"), tr("No bots yet. Create one with /bot new."))
		}
		lines := make([]string, 0, len(bots))
		for _, b := range bots {
			l := "@" + b.Username
			if b.DisplayName != "" {
				l += "  " + b.DisplayName
			}
			if b.Description != "" {
				l += " · " + b.Description
			}
			if b.DeleteAt > 0 {
				l += " (" + tr("disabled") + ")"
			}
			lines = append(lines, l)
		}
		return done(tr("Bots"), lines...)
	}
}

func loadBots(m *Model) tea.Cmd {
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		bots, _ := m.c.Bots(cx)
		var opts []wizOption
		for _, b := range bots {
			if b.DeleteAt == 0 {
				opts = append(opts, wizOption{value: b.Username, label: "@" + b.Username + "  " + b.DisplayName})
			}
		}
		return wizOptionsMsg{options: opts}
	}
}

func runBotToken(m *Model, v wizVals) tea.Cmd {
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		us, err := m.c.UsersByNames(cx, []string{v["bot"]})
		if err != nil {
			return actionDoneMsg{err: err}
		}
		t, err := m.c.CreateToken(cx, us[0].Id, v["description"])
		if err != nil {
			return actionDoneMsg{err: err}
		}
		return actionDoneMsg{title: tr("Create a bot token"), lines: tokenLines(t), copy: t.Token}
	}
}

func runBotAdd(m *Model, v wizVals) tea.Cmd {
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		us, err := m.c.UsersByNames(cx, []string{v["bot"]})
		if err != nil {
			return actionDoneMsg{err: err}
		}
		channel := v["channel"]
		if channel == "" {
			channel = v["_channel"]
		}
		// adding to a team the bot is already in is harmless
		_ = m.c.AddToTeam(cx, v["_team"], us[0].Id)
		if err := m.c.AddToChannel(cx, channel, us[0].Id); err != nil {
			return actionDoneMsg{err: err}
		}
		return done(tr("Add a bot to a channel"), fmt.Sprintf(tr("@%s was added."), us[0].Username))
	}
}

func runTokenNew(m *Model, v wizVals) tea.Cmd {
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		t, err := m.c.CreateToken(cx, m.c.Me.Id, v["description"])
		if err != nil {
			return actionDoneMsg{err: err}
		}
		return actionDoneMsg{title: tr("Create a personal access token"), lines: tokenLines(t), copy: t.Token}
	}
}

func runTokenList(m *Model, v wizVals) tea.Cmd {
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		ts, err := m.c.Tokens(cx)
		if err != nil {
			return actionDoneMsg{err: err}
		}
		if len(ts) == 0 {
			return done(tr("Your access tokens"), tr("No tokens. Create one with /token new."))
		}
		lines := make([]string, 0, len(ts))
		for _, t := range ts {
			state := tr("active")
			if !t.IsActive {
				state = tr("disabled")
			}
			lines = append(lines, t.Id+"  "+t.Description+" ("+state+")")
		}
		return done(tr("Your access tokens"), lines...)
	}
}

func loadTokens(m *Model) tea.Cmd {
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		ts, _ := m.c.Tokens(cx)
		var opts []wizOption
		for _, t := range ts {
			opts = append(opts, wizOption{value: t.Id, label: t.Description})
		}
		return wizOptionsMsg{options: opts}
	}
}

func runTokenRevoke(m *Model, v wizVals) tea.Cmd {
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		if err := m.c.RevokeToken(cx, v["token"]); err != nil {
			return actionDoneMsg{err: err}
		}
		return done(tr("Revoke a token"), tr("The token was revoked."))
	}
}
