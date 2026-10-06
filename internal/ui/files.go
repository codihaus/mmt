package ui

import (
	"bytes"
	"fmt"
	"image"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/codihaus/mmt/internal/mm"
)

// The file picker behind /upload: a small file manager in the spirit of
// yazi and fzf. It lists a folder with the newest files first, previews the
// one under the cursor, and typing searches the folder and the folders
// below it. Tab marks several files, from as many folders as needed.

const (
	walkLimit = 20000 // entries scanned for a search below the folder
	walkDepth = 6
	maxShown  = 300 // search results kept
)

type fileEntry struct {
	path string
	rel  string // shown name; a path below the folder for search results
	dir  bool
	size int64
	mod  time.Time
}

type filePicker struct {
	dir     string
	entries []fileEntry // the folder itself
	tree    []fileEntry // everything below it, for search; nil until walked
	walking bool
	shown   []fileEntry
	idx     int
	top     int
	ti      textinput.Model
	marked  []string
	err     string
}

type walkedMsg struct {
	dir     string
	entries []fileEntry
}

// skipDirs are never searched: big, generated or private.
var skipDirs = map[string]bool{
	"node_modules": true, "Library": true, "vendor": true, "target": true,
	"dist": true, "build": true, "__pycache__": true, "Applications": true,
}

func expandHome(p string) string {
	home, _ := os.UserHomeDir()
	switch {
	case p == "~":
		return home
	case strings.HasPrefix(p, "~/"):
		return filepath.Join(home, p[2:])
	}
	return p
}

// prettyPath shortens the home folder to ~.
func prettyPath(p string) string {
	home, _ := os.UserHomeDir()
	if home != "" && (p == home || strings.HasPrefix(p, home+string(filepath.Separator))) {
		return "~" + p[len(home):]
	}
	return p
}

// startUpload runs /upload: a file path attaches it straight away, anything
// else opens the picker, in that folder when one is given.
func (m *Model) startUpload(arg string) tea.Cmd {
	dir := m.uploadDir
	if arg = strings.Trim(strings.TrimSpace(arg), `"'`); arg != "" {
		p := expandHome(arg)
		if !filepath.IsAbs(p) {
			base, _ := os.Getwd()
			p = filepath.Join(base, p)
		}
		fi, err := os.Stat(p)
		if err != nil {
			m.setStatus(tr("Cannot read file: ")+err.Error(), true)
			return nil
		}
		if !fi.IsDir() {
			m.attachFiles([]string{p})
			return nil
		}
		dir = p
	}
	if dir == "" {
		dir, _ = os.Getwd()
		if home, _ := os.UserHomeDir(); dir == "" || dir == "/" {
			dir = home
		}
	}
	ti := textinput.New()
	ti.Placeholder = tr("Type to search this folder and below")
	ti.Prompt = "› "
	ti.Focus()
	m.files = &filePicker{ti: ti}
	m.input.Blur()
	return tea.Batch(textinput.Blink, m.chdir(dir))
}

// chdir lists dir and starts walking below it for search.
func (m *Model) chdir(dir string) tea.Cmd {
	f := m.files
	f.dir, f.err = filepath.Clean(dir), ""
	f.entries, f.tree, f.walking = nil, nil, false
	des, err := os.ReadDir(f.dir)
	if err != nil {
		f.err = err.Error()
	}
	for _, de := range des {
		if e, ok := entryFor(filepath.Join(f.dir, de.Name()), de.Name()); ok {
			f.entries = append(f.entries, e)
		}
	}
	sort.SliceStable(f.entries, func(i, j int) bool {
		a, b := f.entries[i], f.entries[j]
		if a.dir != b.dir {
			return a.dir
		}
		if a.dir {
			return strings.ToLower(a.rel) < strings.ToLower(b.rel)
		}
		return a.mod.After(b.mod)
	})
	f.ti.SetValue("")
	f.filter()
	return nil
}

// entryFor stats path, following symlinks so a linked folder opens.
func entryFor(path, rel string) (fileEntry, bool) {
	fi, err := os.Stat(path)
	if err != nil || (!fi.IsDir() && !fi.Mode().IsRegular()) {
		return fileEntry{}, false
	}
	return fileEntry{path: path, rel: rel, dir: fi.IsDir(), size: fi.Size(), mod: fi.ModTime()}, true
}

func walkCmd(dir string) tea.Cmd {
	return func() tea.Msg {
		var out []fileEntry
		root := strings.Count(dir, string(filepath.Separator))
		_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || p == dir {
				return nil
			}
			name := d.Name()
			if strings.HasPrefix(name, ".") || skipDirs[name] {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if len(out) >= walkLimit {
				return filepath.SkipAll
			}
			rel, _ := filepath.Rel(dir, p)
			e := fileEntry{path: p, rel: rel, dir: d.IsDir()}
			if info, err := d.Info(); err == nil && (d.IsDir() || info.Mode().IsRegular()) {
				e.size, e.mod = info.Size(), info.ModTime()
				out = append(out, e)
			}
			if d.IsDir() && strings.Count(p, string(filepath.Separator))-root >= walkDepth {
				return filepath.SkipDir
			}
			return nil
		})
		return walkedMsg{dir: dir, entries: out}
	}
}

// filter applies the search text. With none, the folder is listed as is,
// without hidden files; typing a name that starts with "." shows them.
func (f *filePicker) filter() {
	q := strings.TrimSpace(f.ti.Value())
	f.idx, f.top = 0, 0
	f.shown = f.shown[:0]
	if q == "" {
		for _, e := range f.entries {
			if !strings.HasPrefix(e.rel, ".") {
				f.shown = append(f.shown, e)
			}
		}
		return
	}
	type hit struct {
		e     fileEntry
		score int
	}
	var hits []hit
	seen := map[string]bool{}
	add := func(list []fileEntry, deep bool) {
		for _, e := range list {
			if seen[e.path] {
				continue
			}
			s := matchScore(q, filepath.Base(e.rel))
			if s == 0 && strings.ContainsAny(q, "/ ") {
				s = matchScore(q, e.rel) / 2
			}
			if s == 0 {
				continue
			}
			if !deep {
				s += 1000 // the folder itself first
			}
			seen[e.path] = true
			hits = append(hits, hit{e, s})
		}
	}
	var here []fileEntry
	for _, e := range f.entries {
		if !strings.HasPrefix(e.rel, ".") || strings.HasPrefix(q, ".") {
			here = append(here, e)
		}
	}
	add(here, false)
	add(f.tree, true)
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].e.mod.After(hits[j].e.mod)
	})
	for _, h := range hits[:min(len(hits), maxShown)] {
		f.shown = append(f.shown, h.e)
	}
}

func (f *filePicker) isMarked(p string) bool {
	for _, x := range f.marked {
		if x == p {
			return true
		}
	}
	return false
}

func (f *filePicker) toggleMark(p string) {
	for i, x := range f.marked {
		if x == p {
			f.marked = append(f.marked[:i], f.marked[i+1:]...)
			return
		}
	}
	f.marked = append(f.marked, p)
}

func (m *Model) filesKey(k tea.KeyMsg) tea.Cmd {
	f := m.files
	var cur *fileEntry
	if f.idx < len(f.shown) {
		cur = &f.shown[f.idx]
	}
	empty := f.ti.Value() == ""
	move := func(d int) tea.Cmd {
		if n := len(f.shown); n > 0 {
			f.idx = min(max(f.idx+d, 0), n-1)
		}
		return nil
	}
	switch k.String() {
	case "esc", "ctrl+c":
		if !empty {
			f.ti.SetValue("")
			f.filter()
			return nil
		}
		return m.closeFiles()
	case "up", "ctrl+p":
		return move(-1)
	case "down", "ctrl+n":
		return move(1)
	case "pgup":
		return move(-10)
	case "pgdown":
		return move(10)
	case "left":
		return m.chdir(filepath.Dir(f.dir))
	case "right":
		if cur != nil && cur.dir {
			return m.chdir(cur.path)
		}
		return nil
	case "backspace":
		if empty {
			return m.chdir(filepath.Dir(f.dir))
		}
	case "~":
		if empty {
			return m.chdir(expandHome("~"))
		}
	case "tab", " ":
		if k.String() == "tab" || empty {
			if cur != nil && !cur.dir {
				f.toggleMark(cur.path)
				return move(1)
			}
			return nil
		}
	case "enter":
		switch {
		case cur != nil && cur.dir:
			return m.chdir(cur.path)
		case len(f.marked) > 0:
			m.attachFiles(f.marked)
			return m.closeFiles()
		case cur != nil:
			m.attachFiles([]string{cur.path})
			return m.closeFiles()
		}
		return nil
	}
	var cmd tea.Cmd
	before := f.ti.Value()
	f.ti, cmd = f.ti.Update(k)
	if f.ti.Value() != before {
		f.filter()
		if f.tree == nil && !f.walking && f.ti.Value() != "" {
			f.walking = true
			return tea.Batch(cmd, walkCmd(f.dir))
		}
	}
	return cmd
}

func (m *Model) onWalked(msg walkedMsg) {
	f := m.files
	if f == nil || f.dir != msg.dir {
		return
	}
	f.tree, f.walking = msg.entries, false
	if f.tree == nil {
		f.tree = []fileEntry{}
	}
	sel := ""
	if f.idx < len(f.shown) {
		sel = f.shown[f.idx].path
	}
	f.filter()
	for i, e := range f.shown {
		if e.path == sel {
			f.idx = i
		}
	}
}

func (m *Model) attachFiles(paths []string) {
	before := len(m.attach)
	for _, p := range paths {
		m.addAttachment(p, false)
	}
	if n := len(m.attach) - before; n > 0 {
		m.uploadDir = filepath.Dir(paths[len(paths)-1])
		m.setStatus(fmt.Sprintf(tr("Attached %d file(s) · Enter sends them"), n), false)
	}
}

func (m *Model) closeFiles() tea.Cmd {
	m.files = nil
	m.focus = focusInput
	return m.input.Focus()
}

// shortAge is a compact modified time: 5m, 3h, 2d, then a date.
func shortAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return tr("now")
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case t.Year() == time.Now().Year():
		return t.Format("Jan 02")
	}
	return t.Format("2006-01")
}

func (m *Model) viewFiles(w, h int) string {
	f := m.files
	bw := min(112, w-4)
	inner := bw - 4
	rows := min(max(h-14, 6), 20)
	listW := inner
	prevW := 0
	if inner >= 72 {
		prevW = inner * 2 / 5
		listW = inner - prevW - 3
	}

	marked := ""
	if n := len(f.marked); n > 0 {
		marked = stOK.Render(fmt.Sprintf(tr("  ● %d marked"), n))
	}
	count := fmt.Sprintf("%d/%d", min(f.idx+1, len(f.shown)), len(f.shown))
	if f.walking {
		count = tr("searching… ") + count
	}
	label := stAccent.Bold(true).Render(tr("upload")) + "  "
	// a long path loses its start, not the folder you are in
	path := prettyPath(f.dir)
	room := max(inner-ansi.StringWidth(label)-ansi.StringWidth(marked)-ansi.StringWidth(count)-1, 8)
	if r := []rune(path); len(r) > room {
		path = "…" + string(r[len(r)-room+1:])
	}
	head := label + stTitle.Render(path) + marked
	head += strings.Repeat(" ", max(inner-ansi.StringWidth(head)-ansi.StringWidth(count), 1)) + stDim.Render(count)

	if f.idx < f.top {
		f.top = f.idx
	}
	if f.idx >= f.top+rows {
		f.top = f.idx - rows + 1
	}
	var list []string
	for i := f.top; i < min(f.top+rows, len(f.shown)); i++ {
		list = append(list, m.fileRow(f.shown[i], i == f.idx, listW))
	}
	switch {
	case f.err != "":
		list = append(list, stErr.Render(ansi.Truncate(f.err, listW, "…")))
	case len(f.shown) == 0 && f.ti.Value() != "":
		list = append(list, stDim.Render(tr("Nothing matches")))
	case len(f.shown) == 0:
		list = append(list, stDim.Render(tr("Empty folder")))
	}
	for len(list) < rows {
		list = append(list, "")
	}
	body := strings.Join(list, "\n")
	if prevW > 0 {
		var cur *fileEntry
		if f.idx < len(f.shown) {
			cur = &f.shown[f.idx]
		}
		prev := previewLines(cur, prevW, rows)
		for len(prev) < rows {
			prev = append(prev, "")
		}
		sep := stDim.Render(strings.TrimSuffix(strings.Repeat(" │ \n", rows), "\n"))
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			lipgloss.NewStyle().Width(listW).Render(body), sep, strings.Join(prev, "\n"))
	}
	return stPopup.Width(bw - 2).Render(head + "\n" + f.ti.View() + "\n\n" + body)
}

func (m *Model) fileRow(e fileEntry, sel bool, w int) string {
	mark := "  "
	if m.files.isMarked(e.path) {
		mark = stOK.Render("● ")
	}
	var name, meta string
	if e.dir {
		name = stAccent.Bold(true).Render(e.rel + "/")
	} else {
		dir, base := filepath.Split(e.rel)
		name = stDim.Render(dir) + base
		meta = humanSize(e.size) + "  " + fmt.Sprintf("%6s", shortAge(e.mod))
	}
	room := w - 2 - ansi.StringWidth(meta) - 1
	name = ansi.Truncate(name, max(room, 8), "…")
	line := mark + name + strings.Repeat(" ", max(w-2-ansi.StringWidth(name)-ansi.StringWidth(meta), 1)) + stDim.Render(meta)
	if sel {
		plain := ansi.Strip(line)
		return stSugSel.Render(plain + strings.Repeat(" ", max(w-ansi.StringWidth(plain), 0)))
	}
	return line
}

// previewLines describes the entry under the cursor: a folder's contents,
// the start of a text file, an image's size, or just the file's details.
func previewLines(e *fileEntry, w, rows int) []string {
	if e == nil {
		return nil
	}
	clip := func(s string) string { return ansi.Truncate(s, w, "…") }
	out := []string{clip(stTitle.Render(filepath.Base(e.path)))}
	if e.dir {
		des, err := os.ReadDir(e.path)
		if err != nil {
			return append(out, stErr.Render(clip(err.Error())))
		}
		out = append(out, stDim.Render(clip(fmt.Sprintf(tr("folder · %d items"), len(des)))), "")
		for _, de := range des {
			if len(out) >= rows {
				break
			}
			if strings.HasPrefix(de.Name(), ".") {
				continue
			}
			if de.IsDir() {
				out = append(out, stAccent.Render(clip(mm.Clean(de.Name())+"/")))
			} else {
				out = append(out, clip(mm.Clean(de.Name())))
			}
		}
		return out
	}
	out = append(out, stDim.Render(clip(humanSize(e.size)+" · "+e.mod.Format("2006-01-02 15:04"))), "")
	fh, err := os.Open(e.path)
	if err != nil {
		return append(out, stErr.Render(clip(err.Error())))
	}
	defer fh.Close()
	if cfg, format, err := image.DecodeConfig(fh); err == nil {
		return append(out, stAccent.Render(clip(fmt.Sprintf(tr("%s image · %d×%d"), strings.ToUpper(format), cfg.Width, cfg.Height))))
	}
	buf := make([]byte, 4096)
	n, _ := fh.ReadAt(buf, 0)
	buf = buf[:n]
	if n == 0 {
		return append(out, stDim.Render(tr("(empty file)")))
	}
	// the 4 KB may end inside a character
	for i := 0; i < 3 && len(buf) > 0 && !utf8.Valid(buf); i++ {
		buf = buf[:len(buf)-1]
	}
	if bytes.IndexByte(buf, 0) >= 0 || !utf8.Valid(buf) {
		return append(out, stDim.Render(tr("binary file")))
	}
	for _, l := range strings.Split(string(buf), "\n") {
		if len(out) >= rows {
			break
		}
		out = append(out, stDim.Render(clip(mm.Clean(strings.ReplaceAll(l, "\t", "  ")))))
	}
	return out
}
