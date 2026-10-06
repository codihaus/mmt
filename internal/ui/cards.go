package ui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattermost/mattermost/server/public/model"

	"github.com/codihaus/mmt/internal/mm"
)

// Bots and integrations post "message attachments": cards with a title,
// text, fields and buttons, kept in the post's props. They are drawn as a
// block behind a colored bar, like the web app. Buttons and menus are shown
// but used on the web, where clicking them takes you.

var hexColor = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}){1,2}$`)

func cardColor(c string) lipgloss.TerminalColor {
	switch c {
	case "good":
		return colOK
	case "warning":
		return colMention
	case "danger":
		return colErr
	}
	if hexColor.MatchString(c) {
		return lipgloss.Color(c)
	}
	return colAccent
}

// cardLines renders a post's message attachments. links holds the URL each
// line opens when clicked.
func (m *Model) cardLines(p *model.Post, width int) (out, links []string) {
	atts := p.Attachments()
	if len(atts) == 0 {
		return nil, nil
	}
	inner := max(width-2, 10)
	web := m.permalink(p)
	for i, a := range atts {
		if a == nil {
			continue
		}
		bar := lipgloss.NewStyle().Foreground(cardColor(mm.Clean(a.Color))).Render("▌ ")
		add := func(line, link string) {
			out = append(out, bar+line)
			links = append(links, link)
		}
		addText := func(key, text string, style func(...string) string) {
			text = strings.TrimSpace(mm.Clean(text))
			if text == "" {
				return
			}
			if md, ok := m.md.render(fmt.Sprintf("%s:%s%d", p.Id, key, i), p.EditAt, text, inner); ok {
				for _, l := range md {
					add(m.highlightMentions(l), "")
				}
				return
			}
			for _, l := range strings.Split(ansi.Wrap(text, inner, ""), "\n") {
				add(style(m.highlightMentions(l)), "")
			}
		}
		plain := lipgloss.NewStyle().Render

		if pre := strings.TrimSpace(mm.Clean(a.Pretext)); pre != "" {
			for _, l := range strings.Split(ansi.Wrap(pre, width, ""), "\n") {
				out = append(out, l)
				links = append(links, "")
			}
		}
		if name := mm.Clean(a.AuthorName); name != "" {
			add(stDim.Render(ansi.Truncate(name, inner, "…")), mm.Clean(a.AuthorLink))
		}
		if title := mm.Clean(a.Title); title != "" {
			link := mm.Clean(a.TitleLink)
			st := stTitle
			if link != "" {
				st = st.Foreground(colAccent)
			}
			for _, l := range strings.Split(ansi.Wrap(title, inner, ""), "\n") {
				add(st.Render(l), link)
			}
		}
		addText("t", a.Text, plain)
		m.cardFields(a.Fields, inner, add)
		if img := mm.Clean(a.ImageURL); img != "" {
			add(stAccent.Render(tr("[image] "))+stDim.Render(tr("↗ click to view")), img)
		}
		if foot := mm.Clean(a.Footer); foot != "" {
			add(stDim.Render(ansi.Truncate(foot, inner, "…")), "")
		}
		if len(a.Actions) > 0 {
			for _, l := range actionLines(a.Actions, inner) {
				add(l, web)
			}
			add(stDim.Render(tr("↗ click a button to use it on the web")), web)
		}
		if a.Title == "" && a.Text == "" && len(a.Fields) == 0 {
			addText("f", a.Fallback, stDim.Render)
		}
	}
	return out, links
}

// cardFields draws "Title: value" pairs; two short fields share a line when
// they fit, as they sit side by side on the web.
func (m *Model) cardFields(fields []*model.MessageAttachmentField, width int, add func(line, link string)) {
	render := func(f *model.MessageAttachmentField) []string {
		title := mm.Clean(f.Title)
		value := strings.TrimSpace(mm.Clean(fmt.Sprint(f.Value)))
		if f.Value == nil {
			value = ""
		}
		if title == "" {
			return strings.Split(ansi.Wrap(value, width, ""), "\n")
		}
		if value == "" {
			return []string{stTitle.Render(title)}
		}
		head := stTitle.Render(title + ": ")
		lines := strings.Split(ansi.Wrap(value, max(width-ansi.StringWidth(title)-2, 10), ""), "\n")
		lines[0] = head + lines[0]
		return lines
	}
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if f == nil {
			continue
		}
		left := render(f)
		if bool(f.Short) && i+1 < len(fields) && fields[i+1] != nil && bool(fields[i+1].Short) && len(left) == 1 {
			right := render(fields[i+1])
			col := width / 2
			if len(right) == 1 && ansi.StringWidth(left[0]) < col && ansi.StringWidth(right[0]) <= width-col {
				pad := strings.Repeat(" ", col-ansi.StringWidth(left[0]))
				add(left[0]+pad+right[0], "")
				i++
				continue
			}
		}
		for _, l := range left {
			add(l, "")
		}
	}
}

// actionLines lays out buttons and menus as [labels], wrapping to the width.
func actionLines(actions []*model.PostAction, width int) []string {
	var lines []string
	cur, curW := "", 0
	for _, a := range actions {
		if a == nil {
			continue
		}
		name := mm.Clean(a.Name)
		if a.Type == model.PostActionTypeSelect {
			name += " ▾"
		}
		label := "[" + name + "]"
		st := stAccent
		switch a.Style {
		case "danger":
			st = stErr
		case "good", "success":
			st = stOK
		case "warning":
			st = stMention
		}
		if a.Disabled {
			st = stDim
		}
		w := ansi.StringWidth(label)
		if curW > 0 && curW+1+w > width {
			lines = append(lines, cur)
			cur, curW = "", 0
		}
		if curW > 0 {
			cur += " "
			curW++
		}
		cur += st.Render(ansi.Truncate(label, width, "…]"))
		curW += min(w, width)
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}
