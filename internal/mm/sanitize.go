package mm

import (
	"strings"

	"github.com/mattermost/mattermost/server/public/model"
)

// Clean strips terminal control characters from text that other people
// control (messages, names, headers, file names). Without this a message
// could carry escape sequences that retitle the window, repaint the screen,
// write the clipboard (OSC 52) or drive iTerm2 (OSC 1337). Newlines and tabs
// are kept; ESC, the other C0 controls, DEL and C1 controls are dropped.
func Clean(s string) string {
	if !hasControl(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if isControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isControl(r rune) bool {
	switch {
	case r == '\n' || r == '\t':
		return false
	case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f:
		return true
	}
	return false
}

func hasControl(s string) bool {
	for _, r := range s {
		if isControl(r) {
			return true
		}
	}
	return false
}

// CleanPost sanitizes every user-controlled string on a post in place.
func CleanPost(p *model.Post) *model.Post {
	if p == nil {
		return nil
	}
	p.Message = Clean(p.Message)
	p.Type = Clean(p.Type)
	if p.Metadata != nil {
		for _, f := range p.Metadata.Files {
			CleanFile(f)
		}
		for _, r := range p.Metadata.Reactions {
			r.EmojiName = Clean(r.EmojiName)
		}
	}
	return p
}

func CleanFile(f *model.FileInfo) {
	if f != nil {
		f.Name = Clean(f.Name)
		f.MimeType = Clean(f.MimeType)
	}
}

func CleanChannel(ch *model.Channel) *model.Channel {
	if ch != nil {
		ch.Name = Clean(ch.Name)
		ch.DisplayName = Clean(ch.DisplayName)
		ch.Header = Clean(ch.Header)
		ch.Purpose = Clean(ch.Purpose)
	}
	return ch
}

func CleanUser(u *model.User) *model.User {
	if u != nil {
		u.Username = Clean(u.Username)
		u.FirstName = Clean(u.FirstName)
		u.LastName = Clean(u.LastName)
		u.Nickname = Clean(u.Nickname)
	}
	return u
}

func cleanTeam(t *model.Team) {
	t.Name = Clean(t.Name)
	t.DisplayName = Clean(t.DisplayName)
}
