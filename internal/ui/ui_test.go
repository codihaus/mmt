package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattermost/mattermost/server/public/model"

	"github.com/codihaus/mmt/internal/config"
	"github.com/codihaus/mmt/internal/i18n"
	"github.com/codihaus/mmt/internal/mm"
)

func TestFold(t *testing.T) {
	for in, want := range map[string]string{"Cơ hội Dự Án": "co hoi du an", "Đăng": "dang", "ABC": "abc"} {
		if got := fold(in); got != want {
			t.Errorf("fold(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMatchScore(t *testing.T) {
	cases := []struct {
		q, text string
		want    int
	}{
		{"co hoi", "Cơ hội Dự Án", 100},
		{"lead", "Tech Lead", 80},
		{"ech", "Tech Lead", 60},
		{"lead tech", "Tech Lead", 50},
		{"tl", "Tech Lead", 20},
		{"xyz", "Tech Lead", 0},
	}
	for _, c := range cases {
		if got := matchScore(c.q, c.text); got != c.want {
			t.Errorf("matchScore(%q, %q) = %d, want %d", c.q, c.text, got, c.want)
		}
	}
}

func TestPastedPaths(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "ảnh test.png")
	b := filepath.Join(dir, "b.txt")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	esc := strings.ReplaceAll(a, " ", `\ `)
	if got := pastedPaths(esc + " " + b); len(got) != 2 || got[0] != a || got[1] != b {
		t.Errorf("escaped paths: %q", got)
	}
	if got := pastedPaths(`'` + a + `'`); len(got) != 1 || got[0] != a {
		t.Errorf("quoted path: %q", got)
	}
	if got := pastedPaths("hello world"); got != nil {
		t.Errorf("plain text treated as paths: %q", got)
	}
	if got := pastedPaths(a + " " + filepath.Join(dir, "missing")); got != nil {
		t.Errorf("missing file accepted: %q", got)
	}
}

func TestReplaceEmoticons(t *testing.T) {
	cases := map[string]string{
		":D like":           "😄 like",
		"see http://x.y :)": "see http://x.y 🙂",
		"a:)b":              "a:)b",
		"```\n:D\n```":      "```\n:D\n```",
	}
	for in, want := range cases {
		if got := replaceEmoticons(in); got != want {
			t.Errorf("replaceEmoticons(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEmojiGlyph(t *testing.T) {
	for name, want := range map[string]string{"+1": "👍", "+1_tone3": "👍", "white_check_mark": "✅", "thinking_face": "🤔", "custom_logo": ":custom_logo:"} {
		if got := emojiGlyph(name); got != want {
			t.Errorf("emojiGlyph(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestImgCells(t *testing.T) {
	cols, rows := imgCells(&model.FileInfo{Width: 1600, Height: 900}, 60)
	if cols > imgMaxCols || rows > imgMaxRows || rows < 2 {
		t.Errorf("wide image: %dx%d", cols, rows)
	}
	cols, rows = imgCells(&model.FileInfo{Width: 300, Height: 3000}, 60)
	if rows != imgMaxRows || cols < 4 {
		t.Errorf("tall image: %dx%d", cols, rows)
	}
}

func TestUpsertKeepsOrderAndReplacesPending(t *testing.T) {
	list := []*model.Post{{Id: "a", CreateAt: 1}, {Id: "c", CreateAt: 3}}
	list = upsert(list, &model.Post{Id: "b", CreateAt: 2})
	if list[1].Id != "b" {
		t.Fatalf("out of order: %v", ids(list))
	}
	list = upsert(list, &model.Post{PendingPostId: "p1", CreateAt: 4})
	list = upsert(list, &model.Post{Id: "d", PendingPostId: "p1", CreateAt: 4})
	if len(list) != 4 || list[3].Id != "d" {
		t.Fatalf("pending not replaced: %v", ids(list))
	}
	list = remove(list, "b")
	if len(list) != 3 || list[1].Id != "c" {
		t.Fatalf("remove: %v", ids(list))
	}
}

func ids(list []*model.Post) []string {
	var out []string
	for _, p := range list {
		out = append(out, p.Id)
	}
	return out
}

func TestMouseJunk(t *testing.T) {
	m := &Model{}
	runes := func(s string, alt bool) tea.KeyMsg {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s), Alt: alt}
	}
	for _, k := range []tea.KeyMsg{runes("[", true), runes("<65;75;52M", false), runes("[<65;75;52M[<64;75;52M", false), runes("<65;7", false), runes("5;52M", false)} {
		if !m.mouseJunk(k) {
			t.Errorf("fragment %q (alt=%v) not dropped", string(k.Runes), k.Alt)
		}
	}
	m.junkAt = time.Time{}
	for _, s := range []string{"<3", "M", "123", "hello [<"} {
		if m.mouseJunk(runes(s, false)) {
			t.Errorf("real input %q dropped", s)
		}
	}
}

// Strings shown through tr(variable) are not seen by the i18n source scan;
// check the action forms and footer hints here.
func TestActionTextTranslated(t *testing.T) {
	i18n.Set("vi")
	defer i18n.Set("en")
	check := func(s string) {
		if s != "" && !i18n.Has(s) {
			t.Errorf("missing vi translation for %q", s)
		}
	}
	for _, a := range actions {
		check(a.title)
		check(a.help)
		for _, f := range a.fields {
			check(f.label)
			check(f.hint)
		}
	}
	for _, c := range localCommands {
		check(c.help)
	}
	for _, s := range []string{"Enter next", "Tab complete", "Shift+Tab back", "Esc cancel", "/ (empty input)", "All commands: channels, people, bots, tokens"} {
		check(s)
	}
}

func TestLockScreen(t *testing.T) {
	m := New(&mm.Client{Me: &model.User{Id: "me"}}, &config.Config{Lock: "passcode"})
	m.w, m.h = 90, 24
	m.lockNow()
	if m.lock == nil {
		t.Fatal("Ctrl+L did not lock")
	}
	view := m.View()
	if !strings.Contains(view, "locked") || strings.Contains(view, "Town Square") {
		t.Errorf("lock screen not shown:\n%s", view)
	}
	title, body := m.noticeText("@alice in dev", "secret plan")
	if title != "mmt" || strings.Contains(body, "secret") {
		t.Errorf("notification leaks content while locked: %q %q", title, body)
	}
	m.onUnlock(unlockMsg{ok: true})
	if m.lock != nil {
		t.Error("unlock did not clear the lock")
	}
}

func TestNewBar(t *testing.T) {
	item := func(id, label string, last, unread, mentions int64, muted bool) *chanItem {
		return &chanItem{
			ch:    &model.Channel{Id: id, TeamId: "t", Type: model.ChannelTypeOpen, LastPostAt: last},
			label: label, unread: unread, mentions: mentions, muted: muted,
		}
	}
	m := &Model{team: "t", cur: "open", items: map[string]*chanItem{
		"open":  item("open", "here", 9, 3, 0, false),
		"ops":   item("ops", "ops", 5, 1, 0, false),
		"zoro":  item("zoro", "a very long channel name", 7, 2, 2, false),
		"quiet": item("quiet", "muted", 8, 4, 0, true),
		"read":  item("read", "read", 6, 0, 0, false),
	}}
	chips, more := m.newBarChips(80)
	if more != 0 || len(chips) != 2 || chips[0].id != "zoro" || chips[1].id != "ops" {
		t.Fatalf("chips %+v, more %d", chips, more)
	}
	if got := m.newBarAt(80, chips[1].x0); got != "ops" {
		t.Errorf("click on ops opened %q", got)
	}
	if got := m.newBarAt(80, 0); got != "" {
		t.Errorf("click on the rule opened %q", got)
	}
	if w := ansi.StringWidth(m.newBar(80, stDim)); w != 80 {
		t.Errorf("bar is %d cells, want 80", w)
	}
	chips, more = m.newBarChips(chips[0].x1 + 6)
	if len(chips) != 1 || more != 1 {
		t.Errorf("narrow: %d chips, more %d", len(chips), more)
	}
	if w := ansi.StringWidth(m.newBar(chips[0].x1+6, stDim)); w != chips[0].x1+6 {
		t.Errorf("narrow bar is %d cells", w)
	}
	if w := ansi.StringWidth(m.newBar(10, stDim)); w != 10 {
		t.Errorf("tiny bar is %d cells", w)
	}
}
