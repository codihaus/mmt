package ui

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/yuin/goldmark-emoji/definition"

	"github.com/codihaus/mmt/internal/mm"
)

var emojiTable = definition.Github()

// Mattermost names that differ from the GitHub shortcode table.
var emojiAliases = map[string]string{
	"thinking_face":                 "thinking",
	"rolling_on_the_floor_laughing": "rofl",
	"face_with_rolling_eyes":        "roll_eyes",
	"hugging_face":                  "hugs",
}

var skinTone = regexp.MustCompile(`_tone[1-5]$`)

// emojiGlyph turns an emoji name into its character; custom emoji stay as
// :name: since the terminal cannot draw them.
func emojiGlyph(name string) string {
	base := skinTone.ReplaceAllString(name, "")
	if a, ok := emojiAliases[base]; ok {
		base = a
	}
	if e, ok := emojiTable.Get(base); ok && len(e.Unicode) > 0 {
		return string(e.Unicode)
	}
	return ":" + name + ":"
}

// reactionLines groups a post's reactions as "👍 2  ✅ 1", wrapped to width.
// Reactions that include the current user are highlighted, like the web.
func (m *Model) reactionLines(p *model.Post, width int) []string {
	if p.Metadata == nil || len(p.Metadata.Reactions) == 0 {
		return nil
	}
	type group struct {
		name  string
		count int
		mine  bool
		first int64
	}
	byName := map[string]*group{}
	for _, r := range p.Metadata.Reactions {
		g := byName[r.EmojiName]
		if g == nil {
			g = &group{name: r.EmojiName, first: r.CreateAt}
			byName[r.EmojiName] = g
		}
		g.count++
		g.mine = g.mine || r.UserId == m.c.Me.Id
		g.first = min(g.first, r.CreateAt)
	}
	groups := make([]*group, 0, len(byName))
	for _, g := range byName {
		groups = append(groups, g)
	}
	// the web client keeps first-reacted order
	sort.Slice(groups, func(i, j int) bool { return groups[i].first < groups[j].first })

	var lines []string
	cur, curW := "", 0
	for _, g := range groups {
		chip := fmt.Sprintf("%s %d", emojiGlyph(g.name), g.count)
		if g.mine {
			chip = stAccent.Bold(true).Render(chip)
		} else {
			chip = stDim.Render(chip)
		}
		w := ansi.StringWidth(chip)
		if curW > 0 && curW+2+w > width {
			lines = append(lines, cur)
			cur, curW = "", 0
		}
		if curW > 0 {
			cur += "  "
			curW += 2
		}
		cur += chip
		curW += w
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// onReaction applies a reaction_added / reaction_removed event to every
// loaded copy of the post.
func (m *Model) onReaction(ev *model.WebSocketEvent, added bool) {
	raw, _ := ev.GetData()["reaction"].(string)
	var r model.Reaction
	if raw == "" || json.Unmarshal([]byte(raw), &r) != nil {
		return
	}
	r.EmojiName = mm.Clean(r.EmojiName)
	apply := func(list []*model.Post) {
		for _, p := range list {
			if p.Id != r.PostId {
				continue
			}
			if p.Metadata == nil {
				p.Metadata = &model.PostMetadata{}
			}
			kept := p.Metadata.Reactions[:0]
			for _, x := range p.Metadata.Reactions {
				if !(x.UserId == r.UserId && x.EmojiName == r.EmojiName) {
					kept = append(kept, x)
				}
			}
			if added {
				kept = append(kept, &r)
			}
			p.Metadata.Reactions = kept
		}
	}
	for _, b := range m.bufs {
		apply(b.posts)
	}
	if m.thread != nil {
		apply(m.thread.posts)
	}
	m.refresh(false)
}
