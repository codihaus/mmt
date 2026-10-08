// Package ui is the Bubble Tea front end: sidebar, message pane, input box,
// quick switcher and autocomplete.
package ui

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattermost/mattermost/server/public/model"

	"github.com/codihaus/mmt/internal/config"
	"github.com/codihaus/mmt/internal/mm"
)

type focus int

const (
	focusInput focus = iota
	focusSidebar
	focusSelect
)

const (
	sidebarWidth  = 28
	maxDMs        = 40
	maxInputLines = 6
	maxSugs       = 8
	reqTimeout    = 15 * time.Second
)

type chanItem struct {
	ch       *model.Channel
	label    string
	team     string
	unread   int64
	mentions int64
	pinned   bool // shown in sidebar even when outside the recent-DM window
	muted    bool
}

type row struct {
	header    string
	cat       string // category id for header rows
	collapsed bool
	item      *chanItem
}

type buffer struct {
	posts   []*model.Post // oldest first
	more    bool
	loading bool
	loaded  bool
}

type threadState struct {
	root    string
	channel string
	posts   []*model.Post
	loading bool
}

// span maps a post to its line range in the rendered viewport content.
type span struct {
	start, end int
	post       *model.Post
}

type Model struct {
	c   *mm.Client
	cfg *config.Config

	w, h int

	teams      map[string]*model.Team
	team       string // team whose sidebar is shown
	lastByTeam map[string]string
	cats       map[string]*model.OrderedSidebarCategories
	collapsed  map[string]bool // local overrides of category collapse state
	items      map[string]*chanItem
	rows       []row

	sideCursor int
	sideOffset int
	sideFollow bool // scroll the sidebar to the cursor on the next layout

	cur    string
	prev   string
	bufs   map[string]*buffer
	thread *threadState
	failed map[string]bool // pending post ids that failed to send

	focus  focus
	panes  [2]*pane // indexed by paneKind
	active paneKind
	input  textarea.Model // draft of the active pane
	draft  textarea.Model // draft of the other pane

	calls    map[string]bool // channel -> call in progress, from Calls events
	imgs     map[string]*imgState
	imgOrder []string // insertion order, for evicting old images
	imgWant  []string

	sw        *switcher
	lock      *lockState   // app lock screen; nil when unlocked
	lastInput time.Time    // last key or mouse event, for the idle lock
	shell     *shellView   // a !command and its output
	react     *reactPicker // emoji picker for a reaction
	delArm    string       // id (or pending id) of the message waiting for Enter to delete
	delAsk    string       // the footer question while delArm is set
	files     *filePicker  // file picker for /upload
	uploadDir string       // where the file picker opened last
	wiz       *wizard      // management command form
	result    *resultView  // outcome of a management command
	showHelp  bool
	attach    []attachment
	md        *markdown

	sugs   []suggestion
	sugIdx int
	sugFor string

	typing     map[string]map[string]time.Time
	lastTyping time.Time
	junkAt     time.Time // last swallowed mouse-report fragment
	textSel    *textSel  // mouse text selection in a message pane
	lastView   map[string]time.Time

	status    string
	statusErr bool
	statusAt  time.Time

	connected    bool
	disconnected bool
	ready        bool
	bootErr      error
}

func New(c *mm.Client, cfg *config.Config) *Model {
	in := newInput()
	in.Focus()

	return &Model{
		c:          c,
		cfg:        cfg,
		teams:      map[string]*model.Team{},
		lastByTeam: map[string]string{},
		cats:       map[string]*model.OrderedSidebarCategories{},
		collapsed:  map[string]bool{},
		items:      map[string]*chanItem{},
		bufs:       map[string]*buffer{},
		failed:     map[string]bool{},
		typing:     map[string]map[string]time.Time{},
		lastView:   map[string]time.Time{},
		imgs:       map[string]*imgState{},
		lastInput:  time.Now(),
		calls:      map[string]bool{},
		panes:      [2]*pane{newPane(), newPane()},
		input:      in,
		draft:      newInput(),
		// query the terminal background now; it cannot be asked once the
		// program owns stdin
		md: newMarkdown(lipgloss.HasDarkBackground()),
	}
}

// ---- messages ----

type bootMsg struct {
	snap      *mm.Snapshot
	err       error
	reconnect bool
}
type postsMsg struct {
	channel string
	before  string // "" for the newest page
	posts   []*model.Post
	more    bool
	err     error
}
type threadMsg struct {
	root  string
	posts []*model.Post
	err   error
}
type usersMsg struct{ err error }
type sentMsg struct {
	pending string
	post    *model.Post
	err     error
	// what was sent, so a failed message can go back into the input
	channel, root, text string
	files               []attachment
}
type dmMsg struct {
	ch  *model.Channel
	err error
}
type chanMsg struct {
	ch  *model.Channel
	err error
}
type autoMsg struct {
	token string
	users []*model.User
}
type catsMsg struct {
	team string
	cats *model.OrderedSidebarCategories
	err  error
}
type statusMsg struct {
	text string
	err  bool
}
type tickMsg time.Time

// WSEventMsg and WSStateMsg are sent into the program by the WebSocket loop.
type WSEventMsg struct{ Ev *model.WebSocketEvent }
type WSStateMsg struct {
	Connected bool
	Err       error
}

func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), reqTimeout)
}

func (m *Model) bootCmd(reconnect bool) tea.Cmd {
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		s, err := m.c.Bootstrap(cx)
		return bootMsg{snap: s, err: err, reconnect: reconnect}
	}
}

func (m *Model) postsCmd(channel, before string) tea.Cmd {
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		posts, more, err := m.c.Posts(cx, channel, before)
		if err == nil {
			err = m.c.EnsureUsers(cx, userIDs(posts))
		}
		return postsMsg{channel: channel, before: before, posts: posts, more: more, err: err}
	}
}

func (m *Model) threadCmd(root string) tea.Cmd {
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		posts, err := m.c.Thread(cx, root)
		if err == nil {
			err = m.c.EnsureUsers(cx, userIDs(posts))
		}
		return threadMsg{root: root, posts: posts, err: err}
	}
}

func (m *Model) usersCmd(ids []string) tea.Cmd {
	if len(m.c.Missing(ids)) == 0 {
		return nil
	}
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		return usersMsg{err: m.c.EnsureUsers(cx, ids)}
	}
}

func (m *Model) viewCmd(channel, prev string) tea.Cmd {
	m.lastView[channel] = time.Now()
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		if err := m.c.View(cx, channel, prev); err != nil {
			return statusMsg{text: tr("Cannot mark as read: ") + err.Error(), err: true}
		}
		return nil
	}
}

func (m *Model) channelCmd(id string) tea.Cmd {
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		ch, err := m.c.Channel(cx, id)
		if err == nil && ch.Type == model.ChannelTypeDirect {
			err = m.c.EnsureUsers(cx, []string{m.c.DMPartner(ch)})
		}
		return chanMsg{ch: ch, err: err}
	}
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func userIDs(posts []*model.Post) []string {
	ids := make([]string, 0, len(posts))
	for _, p := range posts {
		ids = append(ids, p.UserId)
	}
	return ids
}

func (m *Model) Init() tea.Cmd {
	go cleanTempFiles()
	return tea.Batch(m.bootCmd(false), textarea.Blink, tick())
}

func (m *Model) setStatus(text string, isErr bool) {
	m.status, m.statusErr, m.statusAt = text, isErr, time.Now()
}

// ---- channel list ----

func (m *Model) labelFor(ch *model.Channel) string {
	switch ch.Type {
	case model.ChannelTypeDirect:
		id := m.c.DMPartner(ch)
		if id == m.c.Me.Id {
			return m.c.Me.Username + tr(" (you)")
		}
		return m.c.Username(id)
	case model.ChannelTypeGroup:
		return ch.DisplayName
	default:
		if ch.DisplayName != "" {
			return ch.DisplayName
		}
		return ch.Name
	}
}

func isDM(ch *model.Channel) bool {
	return ch.Type == model.ChannelTypeDirect || ch.Type == model.ChannelTypeGroup
}

func (m *Model) applySnapshot(s *mm.Snapshot) {
	m.teams = map[string]*model.Team{}
	for _, t := range s.Teams {
		m.teams[t.Id] = t
	}
	m.cats = s.Categories
	old := m.items
	m.items = map[string]*chanItem{}
	for _, ch := range s.Channels {
		it := &chanItem{ch: ch, label: m.labelFor(ch)}
		if t := m.teams[ch.TeamId]; t != nil {
			it.team = t.DisplayName
		}
		if o := old[ch.Id]; o != nil {
			it.pinned = o.pinned
		}
		if mem := s.Members[ch.Id]; mem != nil {
			it.unread = max(ch.TotalMsgCount-mem.MsgCount, 0)
			it.mentions = mem.MentionCount
			it.muted = mem.NotifyProps[model.MarkUnreadNotifyProp] == model.ChannelMarkUnreadMention
			if ch.Type == model.ChannelTypeDirect && it.unread > it.mentions {
				it.mentions = it.unread
			}
		}
		if ch.Id == m.cur {
			it.unread, it.mentions = 0, 0
		}
		m.items[ch.Id] = it
	}
	if m.teams[m.team] == nil {
		m.team = ""
		if it := m.items[m.cfg.LastChannel]; it != nil && m.teams[it.ch.TeamId] != nil {
			m.team = it.ch.TeamId
		} else if ts := m.sortedTeams(); len(ts) > 0 {
			m.team = ts[0].Id
		}
	}
	m.buildRows()
}

func (m *Model) addChannel(ch *model.Channel) *chanItem {
	it := &chanItem{ch: ch, label: m.labelFor(ch), pinned: true}
	if t := m.teams[ch.TeamId]; t != nil {
		it.team = t.DisplayName
	}
	m.items[ch.Id] = it
	m.buildRows()
	return it
}

func (m *Model) sortedTeams() []*model.Team {
	teams := make([]*model.Team, 0, len(m.teams))
	for _, t := range m.teams {
		teams = append(teams, t)
	}
	sort.Slice(teams, func(i, j int) bool {
		return strings.ToLower(teams[i].DisplayName) < strings.ToLower(teams[j].DisplayName)
	})
	return teams
}

func (m *Model) buildRows() {
	m.rows = m.rows[:0]
	if m.cfg.UnreadsOnly {
		m.buildUnreadRows()
	} else if cats := m.cats[m.team]; cats != nil && len(cats.Categories) > 0 {
		m.buildCategoryRows(cats)
	} else {
		m.buildDefaultRows()
	}
	m.syncSideCursor()
}

// buildUnreadRows is the unread filter: one list of the conversations with
// something new, most recent first, like the web app's Unreads filter. The
// open channel stays listed until you leave it.
func (m *Model) buildUnreadRows() {
	var list []*chanItem
	for _, it := range m.items {
		if (it.ch.TeamId == m.team || isDM(it.ch)) && (m.hasUnread(it) || it.ch.Id == m.cur) {
			list = append(list, it)
		}
	}
	sortByRecent(list)
	// the Unreads button above already says what this list is
	if len(list) == 0 {
		m.rows = append(m.rows, row{header: tr("Nothing unread")})
	}
	for _, it := range list {
		m.rows = append(m.rows, row{item: it})
	}
}

// unreadsChanged re-lists the sidebar when the unread filter depends on
// counts that just changed.
func (m *Model) unreadsChanged() {
	if m.cfg.UnreadsOnly {
		m.buildRows()
	}
}

// toggleUnreads switches the sidebar between all conversations and only
// the unread ones, and remembers the choice.
func (m *Model) toggleUnreads() tea.Cmd {
	on := !m.cfg.UnreadsOnly
	m.cfg.UnreadsOnly = on
	m.sideOffset = 0
	m.sideFollow = true
	m.buildRows()
	if on {
		m.setStatus(keys(tr("Showing unread conversations only · Alt+U shows all")), false)
	} else {
		m.setStatus(tr("Showing all conversations"), false)
	}
	return func() tea.Msg {
		if err := config.Update(func(c *config.Config) { c.UnreadsOnly = on }); err != nil {
			return statusMsg{text: err.Error(), err: true}
		}
		return nil
	}
}

// visibleDM reports whether a DM makes the cut when the DM list is limited.
func (m *Model) visibleDM(i int, it *chanItem) bool {
	return i < maxDMs || it.unread > 0 || it.pinned || it.ch.Id == m.cur
}

// buildCategoryRows mirrors the sidebar categories configured in the web app:
// category order, per-category sorting, collapsed state and custom groups.
func (m *Model) buildCategoryRows(cats *model.OrderedSidebarCategories) {
	byID := map[string]*model.SidebarCategoryWithChannels{}
	for _, c := range cats.Categories {
		byID[c.Id] = c
	}
	order := []string(cats.Order)
	if len(order) == 0 {
		for _, c := range cats.Categories {
			order = append(order, c.Id)
		}
	}
	placed := map[string]bool{}
	for _, id := range order {
		cat := byID[id]
		if cat == nil {
			continue
		}
		var list []*chanItem
		for _, chID := range cat.Channels {
			if it := m.items[chID]; it != nil && !placed[chID] {
				placed[chID] = true
				list = append(list, it)
			}
		}
		dmCat := cat.Type == model.SidebarCategoryDirectMessages
		switch {
		case cat.Sorting == model.SidebarCategorySortAlphabetical:
			sortByLabel(list)
		case cat.Sorting == model.SidebarCategorySortRecent,
			dmCat && cat.Sorting == model.SidebarCategorySortDefault:
			sortByRecent(list)
		}
		collapsed := m.isCollapsed(cat)
		m.appendHeader(row{header: strings.ToUpper(cat.DisplayName), cat: cat.Id, collapsed: collapsed})
		for i, it := range list {
			if collapsed && !m.hasUnread(it) && it.ch.Id != m.cur {
				continue
			}
			if dmCat && !m.visibleDM(i, it) {
				continue
			}
			m.rows = append(m.rows, row{item: it})
		}
	}
	// channels the categories do not know about yet (e.g. just joined)
	var rest []*chanItem
	for _, it := range m.items {
		if !placed[it.ch.Id] && (it.ch.TeamId == m.team || isDM(it.ch)) {
			rest = append(rest, it)
		}
	}
	if len(rest) > 0 {
		sortByLabel(rest)
		m.appendHeader(row{header: tr("OTHER")})
		for _, it := range rest {
			m.rows = append(m.rows, row{item: it})
		}
	}
}

// buildDefaultRows is the fallback for servers without sidebar categories.
func (m *Model) buildDefaultRows() {
	var list, dms []*chanItem
	for _, it := range m.items {
		switch {
		case isDM(it.ch):
			dms = append(dms, it)
		case it.ch.TeamId == m.team:
			list = append(list, it)
		}
	}
	if len(list) > 0 {
		sortByLabel(list)
		m.appendHeader(row{header: tr("CHANNELS")})
		for _, it := range list {
			m.rows = append(m.rows, row{item: it})
		}
	}
	if len(dms) > 0 {
		sortByRecent(dms)
		m.appendHeader(row{header: tr("DIRECT MESSAGES")})
		for i, it := range dms {
			if m.visibleDM(i, it) {
				m.rows = append(m.rows, row{item: it})
			}
		}
	}
}

// hasUnread matches the web client: muted channels only count mentions.
func (m *Model) hasUnread(it *chanItem) bool {
	return it.mentions > 0 || (it.unread > 0 && !it.muted)
}

func (m *Model) appendHeader(r row) {
	if len(m.rows) > 0 {
		m.rows = append(m.rows, row{})
	}
	m.rows = append(m.rows, r)
}

func (m *Model) isCollapsed(cat *model.SidebarCategoryWithChannels) bool {
	if v, ok := m.collapsed[cat.Id]; ok {
		return v
	}
	return cat.Collapsed
}

func (m *Model) toggleCategory(id string) {
	for _, c := range m.cats[m.team].Categories {
		if c.Id == id {
			m.collapsed[id] = !m.isCollapsed(c)
		}
	}
	m.buildRows()
}

func sortByLabel(list []*chanItem) {
	sort.SliceStable(list, func(i, j int) bool {
		return strings.ToLower(list[i].label) < strings.ToLower(list[j].label)
	})
}

func sortByRecent(list []*chanItem) {
	sort.SliceStable(list, func(i, j int) bool { return list[i].ch.LastPostAt > list[j].ch.LastPostAt })
}

// cycleTeam switches the sidebar to the next team and opens its last channel.
func (m *Model) cycleTeam() tea.Cmd {
	teams := m.sortedTeams()
	if len(teams) < 2 {
		return nil
	}
	next := teams[0].Id
	for i, t := range teams {
		if t.Id == m.team {
			next = teams[(i+1)%len(teams)].Id
		}
	}
	m.team = next
	m.buildRows()
	if id := m.lastByTeam[next]; m.items[id] != nil {
		return m.switchTo(id)
	}
	for _, r := range m.rows {
		if r.item != nil && r.item.ch.Name == "town-square" {
			return m.switchTo(r.item.ch.Id)
		}
	}
	for _, r := range m.rows {
		if r.item != nil {
			return m.switchTo(r.item.ch.Id)
		}
	}
	return nil
}

func (m *Model) catsCmd(team string) tea.Cmd {
	if team == "" {
		return nil
	}
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		cats, err := m.c.Categories(cx, team)
		return catsMsg{team: team, cats: cats, err: err}
	}
}

func (m *Model) rowOf(id string) int {
	for i, r := range m.rows {
		if r.item != nil && r.item.ch.Id == id {
			return i
		}
	}
	return -1
}

func (m *Model) syncSideCursor() {
	if m.focus != focusSidebar {
		if i := m.rowOf(m.cur); i >= 0 {
			m.sideCursor = i
			m.sideFollow = true
		}
	}
	m.clampSideCursor()
}

// clampSideCursor keeps the sidebar cursor on an existing channel row after
// the list shrinks (collapsed section, removed channel).
func (m *Model) clampSideCursor() {
	if len(m.rows) == 0 {
		m.sideCursor = 0
		return
	}
	m.sideCursor = min(max(m.sideCursor, 0), len(m.rows)-1)
	if m.rows[m.sideCursor].item == nil {
		m.moveSideCursor(1)
		if m.rows[m.sideCursor].item == nil {
			m.moveSideCursor(-1)
		}
	}
}

// ---- navigation ----

func (m *Model) switchTo(id string) tea.Cmd {
	it := m.items[id]
	if it == nil {
		return nil
	}
	if m.cur != id {
		m.prev = m.cur
	}
	m.cur = id
	if m.thread != nil {
		m.thread = nil
		m.setActive(paneChannel)
		m.draft.Reset()
	}
	m.showHelp = false
	m.focus = focusInput
	m.clearSugs()
	it.unread, it.mentions = 0, 0
	if t := it.ch.TeamId; t != "" && m.teams[t] != nil {
		m.team = t
	}
	m.lastByTeam[m.team] = id
	m.buildRows()
	if m.rowOf(id) < 0 {
		it.pinned = true
		m.buildRows()
	}
	m.cfg.LastChannel = id

	var cmds []tea.Cmd
	b := m.buf(id)
	if !b.loaded && !b.loading {
		b.loading = true
		cmds = append(cmds, m.postsCmd(id, ""))
	}
	cmds = append(cmds, m.viewCmd(id, m.prev), m.input.Focus(), func() tea.Msg {
		if err := config.Update(func(c *config.Config) { c.LastChannel = id }); err != nil {
			return statusMsg{text: err.Error(), err: true}
		}
		return nil
	})
	m.refresh(true)
	return tea.Batch(cmds...)
}

func (m *Model) buf(id string) *buffer {
	b := m.bufs[id]
	if b == nil {
		b = &buffer{}
		m.bufs[id] = b
	}
	return b
}

// stepChannel moves to the previous/next channel in sidebar order.
func (m *Model) stepChannel(dir int, unreadOnly bool) tea.Cmd {
	start := m.rowOf(m.cur)
	n := len(m.rows)
	for k := 1; k <= n; k++ {
		i := ((start+dir*k)%n + n) % n
		r := m.rows[i]
		if r.item == nil || (unreadOnly && !m.hasUnread(r.item)) {
			continue
		}
		return m.switchTo(r.item.ch.Id)
	}
	if unreadOnly {
		m.setStatus(tr("No unread channels left"), false)
	}
	return nil
}

func (m *Model) moveSideCursor(dir int) {
	for i := m.sideCursor + dir; i >= 0 && i < len(m.rows); i += dir {
		if m.rows[i].item != nil {
			m.sideCursor = i
			m.sideFollow = true
			return
		}
	}
}

// removeChannel drops a channel the user left, was removed from, or that was
// deleted, moving away from it if it is open.
func (m *Model) removeChannel(id string) tea.Cmd {
	if m.items[id] == nil {
		return nil
	}
	delete(m.items, id)
	delete(m.bufs, id)
	if m.thread != nil && m.thread.channel == id {
		m.closeThread()
	}
	m.buildRows()
	if m.cur != id {
		return nil
	}
	m.cur = ""
	for _, r := range m.rows {
		if r.item != nil {
			return m.switchTo(r.item.ch.Id)
		}
	}
	return nil
}

func (m *Model) openThread(p *model.Post) tea.Cmd {
	root := p.Id
	if p.RootId != "" {
		root = p.RootId
	}
	if root == "" {
		return nil
	}
	if m.thread == nil || m.thread.root != root {
		m.inputFor(paneThread).Reset()
	}
	m.thread = &threadState{root: root, channel: m.cur, loading: true}
	m.focus = focusInput
	activate := m.setActive(paneThread)
	m.refreshPane(paneThread, true)
	return tea.Batch(m.threadCmd(root), activate, m.input.Focus())
}

func (m *Model) closeThread() {
	m.thread = nil
	m.setActive(paneChannel)
	m.draft.Reset() // the thread draft
	m.focus = focusInput
	m.refresh(false)
}

// ---- posts ----

func upsert(list []*model.Post, p *model.Post) []*model.Post {
	for i, q := range list {
		if (q.Id != "" && q.Id == p.Id) || (q.Id == "" && p.PendingPostId != "" && q.PendingPostId == p.PendingPostId) {
			list[i] = p
			return list
		}
	}
	list = append(list, p)
	// keep chronological order; new posts nearly always land at the end
	for i := len(list) - 1; i > 0 && list[i].CreateAt < list[i-1].CreateAt; i-- {
		list[i], list[i-1] = list[i-1], list[i]
	}
	return list
}

func remove(list []*model.Post, id string) []*model.Post {
	for i, q := range list {
		if q.Id == id {
			return append(list[:i], list[i+1:]...)
		}
	}
	return list
}

func (m *Model) send() tea.Cmd {
	text := strings.TrimSpace(m.input.Value())
	if (text == "" && len(m.attach) == 0) || m.cur == "" {
		return nil
	}
	m.input.Reset()
	m.clearSugs()
	if strings.HasPrefix(text, "/") && len(m.attach) == 0 {
		return m.runCommand(text)
	}
	if r := reactShortcut.FindStringSubmatch(text); r != nil && len(m.attach) == 0 {
		return m.reactToLatest(r[1])
	}
	if strings.HasPrefix(text, "!") && len(m.attach) == 0 {
		return m.runShell(strings.TrimPrefix(text, "!"))
	}
	// "\!" sends a message that really starts with "!"
	if strings.HasPrefix(text, `\!`) {
		text = text[1:]
	}
	files := m.attach
	m.attach = nil
	return m.postMessage(text, files)
}

// postMessage posts text and attachments to the active pane's channel or
// thread, showing it at once and replacing it when the server confirms.
func (m *Model) postMessage(text string, files []attachment) tea.Cmd {
	now := model.GetMillis()
	p := &model.Post{
		ChannelId:     m.cur,
		UserId:        m.c.Me.Id,
		Message:       text,
		CreateAt:      now,
		PendingPostId: m.c.Me.Id + ":" + time.UnixMilli(now).Format("20060102150405.000"),
	}
	if len(files) > 0 {
		p.Metadata = &model.PostMetadata{}
		for _, a := range files {
			p.Metadata.Files = append(p.Metadata.Files, &model.FileInfo{Name: a.name})
		}
	}
	if m.active == paneThread && m.thread != nil {
		p.RootId = m.thread.root
		m.thread.posts = upsert(m.thread.posts, p)
	}
	// replies are shown inline in the channel too
	if b := m.buf(m.cur); b.loaded {
		b.posts = upsert(b.posts, p)
	}
	m.refresh(true)
	out := &model.Post{ChannelId: p.ChannelId, RootId: p.RootId, Message: p.Message, PendingPostId: p.PendingPostId}
	if len(files) > 0 {
		m.setStatus(tr("Uploading…"), false)
		return m.uploadAndSend(out, files)
	}
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		created, err := m.c.Send(cx, out)
		return sentMsg{pending: out.PendingPostId, post: created, err: err, channel: out.ChannelId, root: out.RootId, text: out.Message}
	}
}

func (m *Model) resolvePending(pending string, p *model.Post) {
	fix := func(list []*model.Post) []*model.Post {
		for i, q := range list {
			if q.Id == "" && q.PendingPostId == pending {
				for _, r := range list {
					if r.Id == p.Id {
						// the WebSocket echo already arrived; drop the placeholder
						return append(list[:i], list[i+1:]...)
					}
				}
				list[i] = p
				return list
			}
		}
		return list
	}
	if b := m.bufs[p.ChannelId]; b != nil {
		b.posts = fix(b.posts)
	}
	if m.thread != nil {
		m.thread.posts = fix(m.thread.posts)
	}
}

// restoreDraft puts a failed message back into its input so it can be
// edited and resent, and drops the placeholder. It reports false when that
// input is gone (channel switched, thread closed); the placeholder then stays,
// marked as failed.
func (m *Model) restoreDraft(msg sentMsg) bool {
	var ta *textarea.Model
	switch {
	case msg.root != "" && m.thread != nil && m.thread.root == msg.root:
		ta = m.inputFor(paneThread)
	case msg.root == "" && m.cur == msg.channel:
		ta = m.inputFor(paneChannel)
	default:
		return false
	}
	if ta.Value() != "" {
		return false
	}
	ta.SetValue(msg.text)
	ta.CursorEnd()
	m.attach = append(m.attach, msg.files...)
	drop := func(list []*model.Post) []*model.Post {
		for i, p := range list {
			if p.Id == "" && p.PendingPostId == msg.pending {
				return append(list[:i], list[i+1:]...)
			}
		}
		return list
	}
	if b := m.bufs[msg.channel]; b != nil {
		b.posts = drop(b.posts)
	}
	if m.thread != nil {
		m.thread.posts = drop(m.thread.posts)
	}
	return true
}

// ---- update ----

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.update(msg)
	m.layout()
	return m, tea.Batch(cmd, m.imageCmds())
}

func (m *Model) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		atBottom := m.p().vp.AtBottom()
		m.w, m.h = msg.Width, msg.Height
		m.layout()
		m.refresh(atBottom)
		return nil

	case bootMsg:
		if msg.err != nil {
			if !m.ready {
				m.bootErr = msg.err
			}
			m.setStatus(tr("Failed to load data: ")+msg.err.Error(), true)
			return nil
		}
		m.applySnapshot(msg.snap)
		if !m.ready {
			m.ready = true
			return m.switchTo(m.initialChannel())
		}
		return nil

	case postsMsg:
		b := m.buf(msg.channel)
		b.loading = false
		if msg.err != nil {
			m.setStatus(tr("Failed to load messages: ")+msg.err.Error(), true)
			return nil
		}
		if msg.before == "" {
			// keep optimistic posts that are still in flight
			var pending []*model.Post
			for _, p := range b.posts {
				if p.Id == "" {
					pending = append(pending, p)
				}
			}
			b.posts = msg.posts
			for _, p := range pending {
				b.posts = upsert(b.posts, p)
			}
		} else {
			older := msg.posts
			seen := map[string]bool{}
			for _, p := range b.posts {
				seen[p.Id] = true
			}
			merged := make([]*model.Post, 0, len(older)+len(b.posts))
			for _, p := range older {
				if !seen[p.Id] {
					merged = append(merged, p)
				}
			}
			b.posts = append(merged, b.posts...)
		}
		b.more = msg.more
		b.loaded = true
		if msg.channel == m.cur {
			if msg.before == "" {
				m.refresh(true)
			} else {
				cp := m.panes[paneChannel]
				before := cp.vp.TotalLineCount()
				m.refresh(false)
				cp.vp.SetYOffset(cp.vp.YOffset + cp.vp.TotalLineCount() - before)
			}
		}
		return nil

	case threadMsg:
		if m.thread == nil || m.thread.root != msg.root {
			return nil
		}
		m.thread.loading = false
		if msg.err != nil {
			m.setStatus(tr("Failed to load thread: ")+msg.err.Error(), true)
			return nil
		}
		for _, p := range m.thread.posts {
			if p.Id == "" {
				msg.posts = upsert(msg.posts, p)
			}
		}
		m.thread.posts = msg.posts
		m.refreshPane(paneThread, true)
		return nil

	case usersMsg:
		for _, it := range m.items {
			it.label = m.labelFor(it.ch)
		}
		m.refresh(false)
		return nil

	case sentMsg:
		if msg.err != nil {
			m.setStatus(tr("Send failed: ")+msg.err.Error(), true)
			if !m.restoreDraft(msg) {
				m.failed[msg.pending] = true
			}
		} else {
			m.resolvePending(msg.pending, msg.post)
			if !m.statusErr {
				m.status = ""
			}
		}
		m.refresh(false)
		return nil

	case dmMsg:
		if msg.err != nil {
			m.setStatus(tr("Cannot open DM: ")+msg.err.Error(), true)
			return nil
		}
		if m.items[msg.ch.Id] == nil {
			m.addChannel(msg.ch)
		}
		return tea.Batch(m.switchTo(msg.ch.Id), m.catsCmd(m.team))

	case chanMsg:
		if msg.err == nil && m.items[msg.ch.Id] == nil {
			it := m.addChannel(msg.ch)
			if msg.ch.Id != m.cur {
				it.unread = 1
				if isDM(msg.ch) {
					it.mentions = 1
				}
			}
			team := msg.ch.TeamId
			if team == "" {
				team = m.team
			}
			return m.catsCmd(team)
		}
		return nil

	case walkedMsg:
		m.onWalked(msg)
		return nil

	case deletedMsg:
		m.onDeleted(msg)
		return nil

	case autoMsg:
		if msg.token != m.sugFor {
			return nil
		}
		m.sugs = m.sugs[:0]
		for _, u := range msg.users {
			m.sugs = append(m.sugs, suggestion{insert: "@" + u.Username, label: "@" + u.Username, hint: fullName(u)})
		}
		m.sugIdx = 0
		return nil

	case pasteMsg:
		if msg.err != nil {
			// no image on the clipboard: behave like a normal text paste
			return m.typeInto(tea.KeyMsg{Type: tea.KeyCtrlV})
		}
		for _, f := range msg.files {
			m.addAttachment(f, false)
		}
		if msg.path != "" {
			m.addAttachment(msg.path, true)
		}
		return nil

	case openedMsg:
		if msg.err != nil {
			m.setStatus(tr("Cannot open file: ")+msg.err.Error(), true)
		} else {
			m.status = ""
		}
		return nil

	case swTickMsg:
		if m.sw != nil && msg.seq == m.sw.seq {
			return m.switcherSearchCmd(msg.seq, strings.TrimSpace(m.sw.ti.Value()))
		}
		return nil

	case swRemoteMsg:
		m.applyRemote(msg)
		return nil

	case joinedMsg:
		if msg.err != nil {
			m.setStatus(tr("Cannot join channel: ")+msg.err.Error(), true)
			return nil
		}
		if m.items[msg.ch.Id] == nil {
			m.addChannel(msg.ch)
		}
		return tea.Batch(m.switchTo(msg.ch.Id), m.catsCmd(msg.ch.TeamId))

	case imgMsg:
		m.onImage(msg)
		if msg.err != nil {
			// drop the reserved rows
			m.refresh(false)
		}
		return nil

	case wizOptionsMsg:
		if m.wiz != nil {
			m.wiz.options = msg.options
			if m.wiz.step < len(m.wiz.act.fields) {
				m.wizSuggest()
			}
		}
		return nil

	case wizUsersMsg:
		if m.wiz != nil && msg.query == m.wiz.sugFor {
			m.wiz.sugs = m.wiz.sugs[:0]
			for _, u := range msg.users {
				m.wiz.sugs = append(m.wiz.sugs, wizOption{value: "@" + u.Username, label: "@" + u.Username + "  " + fullName(u)})
			}
			m.wiz.sugIdx = 0
		}
		return nil

	case actionDoneMsg:
		return m.onActionDone(msg)

	case unlockMsg:
		return m.onUnlock(msg)

	case shellDoneMsg:
		if m.shell != nil {
			m.shell.running = false
			m.shell.out, m.shell.code, m.shell.dur = msg.out, msg.code, msg.dur
			if msg.err != nil {
				m.shell.err = msg.err.Error()
			}
			// nothing to read (open, cp, touch…): go straight back to the chat
			if msg.err == nil && msg.code == 0 && msg.out == "" {
				text := "✓ $ " + m.shell.cmd + " · " + msg.dur.Round(10*time.Millisecond).String()
				m.shell = nil
				return tea.Batch(m.input.Focus(), func() tea.Msg { return statusMsg{text: text} })
			}
		}
		return nil

	case catsMsg:
		if msg.err == nil {
			m.cats[msg.team] = msg.cats
			m.buildRows()
		}
		return nil

	case statusMsg:
		m.setStatus(msg.text, msg.err)
		return nil

	case tickMsg:
		now := time.Time(msg)
		for k, users := range m.typing {
			for u, exp := range users {
				if now.After(exp) {
					delete(users, u)
				}
			}
			if len(users) == 0 {
				delete(m.typing, k)
			}
		}
		if m.status != "" && now.Sub(m.statusAt) > 6*time.Second {
			m.status = ""
		}
		return tea.Batch(tick(), m.checkIdleLock(now))

	case WSStateMsg:
		m.connected = msg.Connected
		if !msg.Connected {
			m.disconnected = true
			text := tr("Disconnected, reconnecting…")
			if msg.Err != nil {
				text += " (" + msg.Err.Error() + ")"
			}
			m.setStatus(text, true)
			return nil
		}
		if m.disconnected && m.ready {
			// catch up on whatever happened while offline
			m.disconnected = false
			for id, b := range m.bufs {
				if id != m.cur {
					b.loaded = false
				}
			}
			cmds := []tea.Cmd{m.bootCmd(true)}
			if m.cur != "" {
				m.buf(m.cur).loading = true
				cmds = append(cmds, m.postsCmd(m.cur, ""))
			}
			if m.thread != nil {
				cmds = append(cmds, m.threadCmd(m.thread.root))
			}
			return tea.Batch(cmds...)
		}
		return nil

	case WSEventMsg:
		return m.handleEvent(msg.Ev)

	case tea.MouseMsg:
		m.lastInput = time.Now()
		if m.lock != nil {
			return nil
		}
		return m.handleMouse(msg)

	case tea.KeyMsg:
		m.lastInput = time.Now()
		if m.lock != nil {
			return m.lockKey(msg)
		}
		return m.handleKey(msg)
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return cmd
}

func (m *Model) initialChannel() string {
	if m.items[m.cfg.LastChannel] != nil {
		return m.cfg.LastChannel
	}
	for _, r := range m.rows {
		if r.item != nil && r.item.ch.Name == "town-square" {
			return r.item.ch.Id
		}
	}
	for _, r := range m.rows {
		if r.item != nil {
			return r.item.ch.Id
		}
	}
	return ""
}

func fullName(u *model.User) string {
	return strings.TrimSpace(u.FirstName + " " + u.LastName)
}
