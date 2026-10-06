package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattermost/mattermost/server/public/model"
)

func (m *Model) ensureSideVisible(rows int) {
	if m.sideCursor < m.sideOffset {
		m.sideOffset = m.sideCursor
	} else if m.sideCursor >= m.sideOffset+rows {
		m.sideOffset = m.sideCursor - rows + 1
	}
}

func symbol(ch *model.Channel) string {
	switch ch.Type {
	case model.ChannelTypeOpen:
		return "# "
	case model.ChannelTypePrivate:
		return "◇ "
	case model.ChannelTypeGroup:
		return "& "
	case model.ChannelTypeDirect:
		return "@ "
	}
	return ""
}

func dayLabel(t time.Time) string {
	now := time.Now()
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Date()
	if y1 == y2 && m1 == m2 && d1 == d2 {
		return tr("Today")
	}
	y3, m3, d3 := now.AddDate(0, 0, -1).Date()
	if y1 == y3 && m1 == m3 && d1 == d3 {
		return tr("Yesterday")
	}
	return tr(t.Weekday().String()) + ", " + t.Format(tr("Jan 2, 2006"))
}

// bubble is a run of consecutive posts by one author, drawn in one frame.
type bubble struct {
	first  *model.Post
	mine   bool
	posts  []*model.Post
	blocks [][]string
	links  [][]string // per block line: URL opened on click, or ""
	imgs   []map[int]imgRef
}

func (m *Model) renderPosts(k paneKind, width int) string {
	pn := m.panes[k]
	var posts []*model.Post
	if k == paneThread {
		posts = m.thread.posts
	} else if b := m.bufs[m.cur]; b != nil {
		posts = b.posts
	}
	pn.visible = posts
	pn.links = map[int]string{}
	pn.imgs = nil
	pn.spans = nil
	if pn.sel >= len(posts) {
		pn.sel = len(posts) - 1
	}
	if len(posts) == 0 {
		msg := tr("No messages yet.")
		if (k == paneThread && m.thread.loading) || (k == paneChannel && m.bufs[m.cur] != nil && !m.bufs[m.cur].loaded) {
			msg = tr("Loading…")
		}
		return stDim.Render("  " + msg)
	}
	var selPost *model.Post
	if m.focus == focusSelect && m.active == k && pn.sel >= 0 {
		selPost = posts[pn.sel]
	}
	inThread := k == paneThread
	bw := max(width-2, 12)
	// frames take at most 3/4 of the pane so both sides stay visible
	boxW := max(bw*3/4, min(bw, 30))
	inner := boxW - 4

	var lines []string
	var spans []span
	emit := func(p *model.Post, ls []string, own bool, link string) {
		gutter := "  "
		if p != nil && p == selPost && !own {
			gutter = stGutter.Render("▌ ")
		}
		start := len(lines)
		for _, l := range ls {
			if link == "" {
				link = urlRe.FindString(ansi.Strip(l))
			}
			if link != "" {
				pn.links[len(lines)] = link
			}
			lines = append(lines, gutter+l)
			link = ""
		}
		if p == nil {
			return
		}
		if n := len(spans); n > 0 && spans[n-1].post == p {
			spans[n-1].end = len(lines)
		} else {
			spans = append(spans, span{start: start, end: len(lines), post: p})
		}
	}
	var cur *bubble
	flush := func() {
		if cur == nil {
			return
		}
		if len(lines) > 0 && lines[len(lines)-1] != "" {
			lines = append(lines, "")
		}
		for _, row := range m.frame(cur, inner, inThread, bw, selPost) {
			if row.img != nil {
				pn.imgs = append(pn.imgs, imgPlace{line: len(lines), col: 2 + row.imgCol, cols: row.img.cols, rows: row.img.rows, id: row.img.id})
			}
			emit(row.post, []string{row.text}, cur.mine, row.link)
		}
		cur = nil
	}

	if b := m.bufs[m.cur]; !inThread && b != nil && b.more {
		lines = append(lines, stDim.Render(tr("  ↑ scroll up for older messages")))
	}

	var prevDay string
	var prevAt int64
	for _, p := range posts {
		t := time.UnixMilli(p.CreateAt)
		if day := t.Format("2006-01-02"); day != prevDay {
			flush()
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, lipgloss.PlaceHorizontal(width, lipgloss.Center, stDim.Render(" "+dayLabel(t)+" "),
				lipgloss.WithWhitespaceChars("─"), lipgloss.WithWhitespaceForeground(colBorder)))
			prevDay = day
		}
		if strings.HasPrefix(p.Type, "system_") {
			flush()
			lines = append(lines, "")
			var ls []string
			for _, l := range m.formatMessage(p, bw, true) {
				ls = append(ls, lipgloss.PlaceHorizontal(bw, lipgloss.Center, l))
			}
			emit(p, ls, false, "")
			continue
		}
		if cur == nil || cur.first.UserId != p.UserId || cur.first.RootId != p.RootId || p.CreateAt-prevAt > 5*60*1000 {
			flush()
			cur = &bubble{first: p, mine: p.UserId == m.c.Me.Id}
		}
		block := m.formatMessage(p, inner, false)
		links := make([]string, len(block))
		if p.Type == callPostType {
			var link string
			block, link = m.callLines(p)
			links = []string{link, link}
		}
		cards, cardLinks := m.cardLines(p, inner)
		block = append(block, cards...)
		links = append(links, cardLinks...)
		files, fileLinks, fileImgs := m.attachmentLines(p, inner)
		var imgs map[int]imgRef
		for i, r := range fileImgs {
			if imgs == nil {
				imgs = map[int]imgRef{}
			}
			imgs[len(block)+i] = r
		}
		block = append(block, files...)
		links = append(links, fileLinks...)
		if p.Id == "" && m.failed[p.PendingPostId] {
			block = append(block, stErr.Render(tr("(failed to send)")))
		}
		block = append(block, m.reactionLines(p, inner)...)
		if !inThread && p.RootId == "" && p.ReplyCount > 0 {
			block = append(block, stAccent.Render(fmt.Sprintf(tr("↳ %d replies"), p.ReplyCount)))
		}
		for len(links) < len(block) {
			links = append(links, "")
		}
		cur.posts = append(cur.posts, p)
		cur.blocks = append(cur.blocks, block)
		cur.links = append(cur.links, links)
		cur.imgs = append(cur.imgs, imgs)
		prevAt = p.CreateAt
	}
	flush()
	pn.spans = spans
	return strings.Join(lines, "\n")
}

type frameRow struct {
	post   *model.Post
	text   string
	link   string
	img    *imgRef
	imgCol int // column of the frame content, for placing img
}

// frame draws a bubble as a rounded box with the author and time set into the
// top border. Own messages sit on the right, everyone else on the left.
func (m *Model) frame(b *bubble, maxInner int, inThread bool, bw int, sel *model.Post) []frameRow {
	var label string
	at := stDim.Render(time.UnixMilli(b.first.CreateAt).Format("15:04"))
	if b.mine {
		label = at
	} else {
		label = userStyle(b.first.UserId).Render(m.c.Username(b.first.UserId)) + stDim.Render(" · ") + at
	}
	if b.first.RootId != "" && !inThread {
		label += stDim.Render(" · ↳ thread")
	}
	labelW := ansi.StringWidth(label)

	// wrap anything wider than the frame (e.g. long code lines)
	var body, bodyLinks [][]string
	var bodyImgs []map[int]imgRef
	w := 0
	for bi, blk := range b.blocks {
		var out, outLinks []string
		var outImgs map[int]imgRef
		for li, l := range blk {
			parts := []string{l}
			if ansi.StringWidth(l) > maxInner {
				parts = strings.Split(ansi.Wrap(l, maxInner, ""), "\n")
			}
			if r, ok := b.imgs[bi][li]; ok {
				if outImgs == nil {
					outImgs = map[int]imgRef{}
				}
				outImgs[len(out)] = r
			}
			for _, part := range parts {
				out = append(out, part)
				outLinks = append(outLinks, b.links[bi][li])
			}
		}
		for _, l := range out {
			w = max(w, ansi.StringWidth(l))
		}
		body = append(body, out)
		bodyLinks = append(bodyLinks, outLinks)
		bodyImgs = append(bodyImgs, outImgs)
	}
	inner := min(max(w, labelW+2), maxInner)
	if labelW+2 > inner {
		label = ansi.Truncate(label, inner-2, "…")
		labelW = ansi.StringWidth(label)
	}

	bc := lipgloss.NewStyle().Foreground(colBorder)
	switch {
	case b.mine:
		bc = lipgloss.NewStyle().Foreground(colAccent)
	case m.mentionsMe(b.posts):
		bc = lipgloss.NewStyle().Foreground(colMention)
	}
	h := inner + 2
	fill := strings.Repeat("─", h-labelW-3)
	var top string
	if b.mine {
		top = bc.Render("╭"+fill+" ") + label + bc.Render(" ─╮")
	} else {
		top = bc.Render("╭─ ") + label + bc.Render(" "+fill+"╮")
	}
	bottom := bc.Render("╰" + strings.Repeat("─", h) + "╯")
	side := bc.Render("│")

	indent := ""
	if b.mine {
		indent = strings.Repeat(" ", max(bw-(inner+4), 0))
	}
	rows := []frameRow{{post: b.posts[0], text: indent + top}}
	for i, blk := range body {
		for li, l := range blk {
			pad := strings.Repeat(" ", max(inner-ansi.StringWidth(l), 0))
			row := frameRow{post: b.posts[i], text: indent + side + " " + l + pad + " " + side, link: bodyLinks[i][li]}
			if r, ok := bodyImgs[i][li]; ok {
				row.img, row.imgCol = &r, len(indent)+2
			}
			rows = append(rows, row)
		}
	}
	rows = append(rows, frameRow{post: b.posts[len(b.posts)-1], text: indent + bottom})
	if b.mine && len(indent) >= 2 {
		// the selection marker hugs the right-aligned frame instead of the far edge
		for i := range rows {
			if rows[i].post == sel {
				rows[i].text = indent[:len(indent)-2] + stGutter.Render("▌ ") + rows[i].text[len(indent):]
			}
		}
	}
	return rows
}

func (m *Model) mentionsMe(posts []*model.Post) bool {
	me := "@" + m.c.Me.Username
	for _, p := range posts {
		if strings.Contains(p.Message, me) || strings.Contains(p.Message, "@channel") || strings.Contains(p.Message, "@here") || strings.Contains(p.Message, "@all") {
			return true
		}
	}
	return false
}

func (m *Model) formatMessage(p *model.Post, width int, sys bool) []string {
	text := p.Message
	if text == "" && sys {
		text = p.Type
	}
	if !sys {
		if strings.TrimSpace(text) == "" {
			// only attachments or files; they are drawn below
			return nil
		}
		text = replaceEmoticons(text)
	}
	var out []string
	rendered := false
	if !sys && p.Id != "" {
		if md, ok := m.md.render(p.Id, p.EditAt, text, width); ok {
			rendered = true
			out = make([]string, len(md))
			for i, l := range md {
				out[i] = m.highlightMentions(l)
			}
		}
	}
	inCode := false
	for _, raw := range strings.Split(text, "\n") {
		if rendered {
			break
		}
		if strings.HasPrefix(strings.TrimSpace(raw), "```") {
			inCode = !inCode
			continue
		}
		style := lipgloss.NewStyle().Render
		switch {
		case sys:
			style = stDim.Italic(true).Render
		case p.Id == "":
			style = stDim.Render
		case inCode:
			style = stCode.Render
		}
		line := raw
		if !inCode && !sys {
			line = m.highlightMentions(line)
		}
		for _, w := range strings.Split(ansi.Wrap(line, width, ""), "\n") {
			out = append(out, style(w))
		}
	}
	if p.EditAt > 0 && len(out) > 0 {
		out[len(out)-1] += stDim.Render(tr(" (edited)"))
	}
	return out
}

func (m *Model) highlightMentions(s string) string {
	if !strings.Contains(s, "@") {
		return s
	}
	for _, name := range []string{"@" + m.c.Me.Username, "@channel", "@here", "@all"} {
		s = strings.ReplaceAll(s, name, stMention.Render(name))
	}
	return s
}

// ---- frame ----

func (m *Model) View() string {
	if m.w == 0 {
		return ""
	}
	if m.lock != nil {
		return m.viewLock()
	}
	if !m.ready {
		if m.bootErr != nil {
			return stErr.Render(tr("Cannot connect: ")+m.bootErr.Error()) + "\n" + stDim.Render(tr("Ctrl+C to quit. Run `mmt login` if the token expired."))
		}
		return stDim.Render(tr("Connecting to Mattermost…"))
	}
	g := m.geo()
	main := m.viewMain(g)
	body := main
	if g.sideW > 0 {
		body = lipgloss.JoinHorizontal(lipgloss.Top, m.viewSidebar(g), main)
	}
	return body + "\n" + m.viewFooter() + m.imageOverlay(g, strings.Split(body, "\n"))
}

func (m *Model) viewSidebar(g geo) string {
	dot := stOK.Render("●")
	if !m.connected {
		dot = stErr.Render("● offline")
	}
	teamLine := ""
	if t := m.teams[m.team]; t != nil {
		teamLine = " " + stAccent.Bold(true).Render(t.DisplayName)
		if len(m.teams) > 1 {
			teamLine += stDim.Render(keys(tr("  Ctrl+T switch")))
		}
	}
	lines := []string{
		" " + stTitle.Render("mmt") + " " + stDim.Render("@"+m.c.Me.Username) + " " + dot,
		ansi.Truncate(teamLine, sidebarWidth, "…"),
		ansi.Truncate(m.unreadsButton(), sidebarWidth, "…"),
	}
	end := min(m.sideOffset+g.sideRows, len(m.rows))
	for i := m.sideOffset; i < end; i++ {
		lines = append(lines, m.sideRow(i))
	}
	col := lipgloss.NewStyle().
		Width(sidebarWidth).
		Height(m.h-1).
		MaxHeight(m.h-1).
		Border(lipgloss.NormalBorder(), false, true, false, false).
		BorderForeground(colBorder)
	return col.Render(strings.Join(lines, "\n"))
}

// unreadsButton is the clickable switch for the unread filter, with the
// number of conversations that have something new.
func (m *Model) unreadsButton() string {
	n := 0
	for _, it := range m.items {
		if (it.ch.TeamId == m.team || isDM(it.ch)) && m.hasUnread(it) {
			n++
		}
	}
	label := tr("Unreads")
	if n > 0 {
		label += fmt.Sprintf(" %d", n)
	}
	hint := stDim.Render(keys("  Alt+U"))
	if m.cfg.UnreadsOnly {
		return " " + stMention.Render("● "+label) + hint
	}
	return " " + stSideNormal.Render("○ "+label) + hint
}

func (m *Model) sideRow(i int) string {
	r := m.rows[i]
	if r.item == nil {
		if r.header == "" {
			return ""
		}
		if r.cat == "" {
			return " " + stSection.Render(r.header)
		}
		arrow := "▾ "
		if r.collapsed {
			arrow = "▸ "
		}
		return " " + stSection.Render(ansi.Truncate(arrow+r.header, sidebarWidth-1, "…"))
	}
	it := r.item
	badge := ""
	switch {
	case it.mentions > 0:
		badge = stBadge.Render(fmt.Sprintf(" %d ", it.mentions))
	case it.unread > 0 && !it.muted:
		badge = stAccent.Render("●")
	}
	avail := sidebarWidth - 2 - ansi.StringWidth(badge)
	text := ansi.Truncate(symbol(it.ch)+it.label, avail, "…")
	text += strings.Repeat(" ", max(avail-ansi.StringWidth(text), 0))
	st := stSideNormal
	switch {
	case m.focus == focusSidebar && i == m.sideCursor:
		st = stSideCursor
	case it.ch.Id == m.cur:
		st = stSideCur
	case it.muted:
		st = stSideMuted
	case it.unread > 0:
		st = stSideUnread
	}
	return " " + st.Render(text) + badge
}

func (m *Model) viewSugs(w, n int) string {
	lines := make([]string, 0, n)
	start := min(max(m.sugIdx-n+1, 0), max(len(m.sugs)-n, 0))
	for j, s := range m.sugs[start : start+n] {
		i := start + j
		text := " " + s.label
		if s.hint != "" {
			text += "  " + stDim.Render(s.hint)
		}
		text = ansi.Truncate(text, w-2, "…")
		if i == m.sugIdx {
			text = stSugSel.Render(text + strings.Repeat(" ", max(w-2-ansi.StringWidth(text), 0)))
		}
		lines = append(lines, " "+text)
	}
	return strings.Join(lines, "\n")
}

func (m *Model) viewFooter() string {
	var parts []string
	switch {
	case m.result != nil:
		parts = []string{"Esc close"}
	case m.shell != nil:
		parts = []string{"Enter send output", "c copy", "Esc close"}
	case m.react != nil:
		parts = []string{"1-8 or ↑↓ Enter react", "type to search", "Esc close"}
	case m.files != nil:
		parts = []string{"↑↓ move", "Enter open folder / attach", "Tab mark", "← parent folder", "~ home", "type to search", "Esc close"}
	case m.focus == focusInput && strings.HasPrefix(m.input.Value(), "!"):
		parts = []string{"Enter runs this on your computer (not sent to the chat)", "\\! sends a message starting with !"}
	case m.wiz != nil:
		parts = []string{"Enter next", "Tab complete", "Shift+Tab back", "Esc cancel"}
	case m.sw != nil:
		parts = []string{"↑↓ select", "Enter open", "Esc close"}
	case m.showHelp:
		parts = []string{"Esc close"}
	case m.focus == focusSidebar:
		parts = []string{"↑↓ move", "Enter open channel", "Tab/Esc back to input"}
	case m.focus == focusSelect && m.active == paneThread:
		parts = []string{"↑↓ select message", "Enter reply", "e react", "c copy", "o open file", "y copy link", "w open on web", "Esc back to input"}
	case m.focus == focusSelect:
		parts = []string{"↑↓ select message", "Enter open thread", "e react", "c copy", "o open file", "y copy link", "w open on web", "Esc back to input"}
	case len(m.sugs) > 0:
		parts = []string{"↑↓ select", "Tab/Enter complete", "Esc dismiss"}
	case m.active == paneThread:
		parts = []string{"Enter reply", "Shift+Enter new line", "↑ select message", "Esc close thread", "Tab switch pane", "F1 help"}
	case m.thread != nil:
		parts = []string{"Enter send", "Shift+Enter new line", "↑ select message", "Tab switch pane", "Ctrl+K switch channel", "F1 help"}
	default:
		parts = []string{"Enter send", "Shift+Enter new line", "Ctrl+V paste image", "Ctrl+K switch channel", "Alt+↑↓ next channel", "↑ select message", "Tab sidebar", "F1 help"}
	}
	split := m.geo().split
	shown := parts[:0]
	for _, p := range parts {
		if p == "Tab switch pane" && !split {
			continue
		}
		shown = append(shown, tr(p))
	}
	return ansi.Truncate(stDim.Render(" "+keys(strings.Join(shown, " · "))), m.w, "…")
}

func helpBox() string {
	rows := [][2]string{
		{"/ (empty input)", "All commands: channels, people, bots, tokens"},
		{"!command", "Run a shell command on this computer"},
		{"Enter", "Send"},
		{"Shift+Enter", "New line (or \\ then Enter, Ctrl+J)"},
		{"Ctrl+K", "Find & switch channel or DM"},
		{"Ctrl+T / click team name", "Switch team"},
		{"Click a section", "Collapse or expand a sidebar section"},
		{"Ctrl+V", "Paste an image from the clipboard"},
		{"Drop a file", "Attach it"},
		{"/upload", "Pick files to attach in a file browser"},
		{"Click [image]/[file]/link", "Open in the browser"},
		{"o (message selected)", "Open attachments with the default app"},
		{"c (message selected)", "Copy the message text"},
		{"e (message selected)", "Add or remove a reaction"},
		{"+:emoji:", "React to the latest message, e.g. +:thumbsup:"},
		{"y / w (message selected)", "Copy link / open the message on the web"},
		{"Drag over messages", "Select text; it is copied on release"},
		{"/link  /open", "Copy link / open the channel or thread on the web"},
		{"Alt+↑ / Alt+↓", "Previous / next channel (or Ctrl+P / Ctrl+N)"},
		{"Alt+A", "Next unread channel"},
		{"Alt+U", "Show only unread conversations, or all again"},
		{"Tab / Shift+Tab", "Switch pane: sidebar → channel → thread"},
		{"↑ (empty input)", "Select messages; Enter opens the thread"},
		{"Click", "Open a channel; click a message twice for its thread"},
		{"PgUp / PgDn, mouse wheel", "Scroll messages"},
		{"Esc", "Close popup or thread"},
		{"@ / ~ / /", "Suggest people / channels / commands"},
		{"/dm @user", "Open a direct message"},
		{"Ctrl+L / /lock", "Lock mmt (Touch ID or passcode; set up with mmt lock)"},
		{"Ctrl+C", "Clear the input; again to quit"},
	}
	var b strings.Builder
	b.WriteString(stTitle.Render(tr("Shortcuts")) + "\n\n")
	for _, r := range rows {
		b.WriteString(stAccent.Render(fmt.Sprintf("%-26s", keys(tr(r[0])))) + keys(tr(r[1])) + "\n")
	}
	b.WriteString("\n" + stDim.Render(tr("Hold Option (iTerm2) or Fn (Terminal) while dragging to select text.")))
	return stPopup.Render(b.String())
}
