package mm

import (
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
)

func TestCleanDropsEscapes(t *testing.T) {
	cases := map[string]string{
		"hello":                           "hello",
		"xin chào\nline 2\tx":             "xin chào\nline 2\tx",
		"title\x1b]0;pwned\x07!":          "title]0;pwned!",
		"clip\x1b]52;c;ZXZpbA==\x07":      "clip]52;c;ZXZpbA==",
		"clear\x1b[2J\x1b[H":              "clear[2J[H",
		"c1\u009b31mred":                  "c131mred",
		"del\x7f":                         "del",
		"iterm\x1b]1337;SetProfile=x\x07": "iterm]1337;SetProfile=x",
	}
	for in, want := range cases {
		if got := Clean(in); got != want {
			t.Errorf("Clean(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCleanPostCoversMetadata(t *testing.T) {
	p := &model.Post{Message: "a\x1b[2Jb", Metadata: &model.PostMetadata{
		Files:     []*model.FileInfo{{Name: "x\x1b]0;t\x07.png"}},
		Reactions: []*model.Reaction{{EmojiName: "+1\x1b[0m"}},
	}}
	CleanPost(p)
	if p.Message != "a[2Jb" || p.Metadata.Files[0].Name != "x]0;t.png" || p.Metadata.Reactions[0].EmojiName != "+1[0m" {
		t.Errorf("not cleaned: %+v %+v %+v", p.Message, p.Metadata.Files[0].Name, p.Metadata.Reactions[0].EmojiName)
	}
}

func TestDMPartner(t *testing.T) {
	c := &Client{Me: &model.User{Id: "me"}}
	for name, want := range map[string]string{"me__you": "you", "you__me": "you", "me__me": "me", "town-square": ""} {
		if got := c.DMPartner(&model.Channel{Name: name}); got != want {
			t.Errorf("DMPartner(%q) = %q, want %q", name, got, want)
		}
	}
}
