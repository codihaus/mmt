package ui

import (
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattermost/mattermost/server/public/model"
)

var urlRe = regexp.MustCompile(`https?://[^\s<>"'()]+`)

func (m *Model) teamName(teamID string) string {
	if t := m.teams[teamID]; t != nil {
		return t.Name
	}
	if t := m.teams[m.team]; t != nil {
		return t.Name
	}
	return ""
}

// channelURL is the web client address of a channel or DM.
func (m *Model) channelURL(it *chanItem) string {
	base := m.c.Server() + "/" + m.teamName(it.ch.TeamId)
	switch it.ch.Type {
	case model.ChannelTypeDirect:
		return base + "/messages/@" + m.c.Username(m.c.DMPartner(it.ch))
	case model.ChannelTypeGroup:
		return base + "/messages/" + it.ch.Name
	}
	return base + "/channels/" + it.ch.Name
}

func (m *Model) permalink(p *model.Post) string {
	team := ""
	if it := m.items[p.ChannelId]; it != nil {
		team = it.ch.TeamId
	}
	return m.c.Server() + "/" + m.teamName(team) + "/pl/" + p.Id
}

// fileURL opens images inline in the browser (preview endpoint) and downloads
// anything else. It relies on the browser's web-client session cookie.
func (m *Model) fileURL(f *model.FileInfo) string {
	u := m.c.Server() + "/api/v4/files/" + f.Id
	if strings.HasPrefix(f.MimeType, "image/") || f.HasPreviewImage {
		return u + "/preview"
	}
	return u + "?download=1"
}

// currentURL is what /open and /link act on: the open thread, else the channel.
func (m *Model) currentURL() string {
	if m.active == paneThread && m.thread != nil {
		team := ""
		if it := m.items[m.thread.channel]; it != nil {
			team = it.ch.TeamId
		}
		return m.c.Server() + "/" + m.teamName(team) + "/pl/" + m.thread.root
	}
	if it := m.items[m.cur]; it != nil {
		return m.channelURL(it)
	}
	return ""
}

func (m *Model) openURLCmd(url string) tea.Cmd {
	browser := m.cfg.Browser
	return func() tea.Msg {
		if err := openExternal(url, browser); err != nil {
			return statusMsg{text: tr("Cannot open link: ") + err.Error(), err: true}
		}
		return statusMsg{text: tr("Opened ") + url}
	}
}

func copyCmd(text string) tea.Cmd {
	return func() tea.Msg {
		if err := copyText(text); err != nil {
			return statusMsg{text: tr("Cannot copy: ") + err.Error(), err: true}
		}
		return statusMsg{text: tr("Copied ") + ansi.Truncate(strings.Join(strings.Fields(text), " "), 60, "…")}
	}
}

// attachmentLines renders a post's files. links holds the URL each line
// opens when clicked; imgs marks rows reserved for an inline image.
func (m *Model) attachmentLines(p *model.Post, maxCols int) (out, links []string, imgs map[int]imgRef) {
	if p.Metadata != nil && len(p.Metadata.Files) > 0 {
		for _, f := range p.Metadata.Files {
			isImg := strings.HasPrefix(f.MimeType, "image/")
			kind := tr("[file] ")
			if isImg {
				kind = tr("[image] ")
			}
			if f.Id == "" {
				// still uploading
				out = append(out, stAccent.Render(kind)+f.Name)
				links = append(links, "")
				continue
			}
			url := m.fileURL(f)
			if isImg && inlineImages && (m.imgs[f.Id] == nil || !m.imgs[f.Id].failed) {
				m.wantImage(f.Id)
				cols, rows := imgCells(f, maxCols)
				if imgs == nil {
					imgs = map[int]imgRef{}
				}
				imgs[len(out)] = imgRef{id: f.Id, cols: cols, rows: rows}
				for range rows {
					out = append(out, strings.Repeat(" ", cols))
					links = append(links, url)
				}
				out = append(out, stDim.Render(f.Name+tr("  ↗ click to open")))
				links = append(links, url)
				continue
			}
			out = append(out, stAccent.Render(kind)+stLink.Render(f.Name)+stDim.Render(tr("  ↗ click to view")))
			links = append(links, url)
		}
	} else if len(p.FileIds) > 0 {
		out = append(out, stAccent.Render(tr("[attachment]"))+stDim.Render(tr("  ↗ click to open the message on the web")))
		links = append(links, m.permalink(p))
	}
	return out, links, imgs
}
