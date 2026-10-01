package ui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattermost/mattermost/server/public/model"
)

const maxFiles = 10 // Mattermost limit per post

type attachment struct {
	path string
	name string
	size int64
	temp bool // created by us from the clipboard; removed after upload
}

type pasteMsg struct {
	files []string // files copied in Finder
	path  string   // clipboard image written to a temp PNG
	err   error
}

type openedMsg struct{ err error }

var errNoImage = errors.New("clipboard has no image")

// clipboardFilesJS lists file URLs on the clipboard (files copied with Cmd+C
// in Finder), one path per line.
const clipboardFilesJS = `ObjC.import("AppKit");
var urls = $.NSPasteboard.generalPasteboard.readObjectsForClassesOptions($([$.NSURL]), $({"NSPasteboardURLReadingFileURLsOnlyKey": true}));
var out = [];
if (urls) { for (var i = 0; i < urls.count; i++) out.push(urls.objectAtIndex(i).path.js); }
out.join("\n")`

// clipboardImageCmd attaches what is on the clipboard: files copied in
// Finder first (a copied file also offers its icon as an image), then an
// image such as a screenshot, written to a temp PNG. Paths go through argv,
// never into script text.
func clipboardImageCmd() tea.Cmd {
	return func() tea.Msg {
		if runtime.GOOS != "darwin" {
			return pasteMsg{err: errNoImage}
		}
		if out, err := exec.Command("osascript", "-l", "JavaScript", "-e", clipboardFilesJS).Output(); err == nil {
			var files []string
			for _, p := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				if fi, err := os.Stat(p); p != "" && err == nil && fi.Mode().IsRegular() {
					files = append(files, p)
				}
			}
			if len(files) > 0 {
				return pasteMsg{files: files}
			}
		}
		path := filepath.Join(os.TempDir(), fmt.Sprintf("mmt-paste-%s.png", time.Now().Format("20060102-150405.000")))
		err := exec.Command("osascript",
			"-e", "on run argv",
			"-e", "set img to (the clipboard as «class PNGf»)",
			"-e", "set f to open for access (POSIX file (item 1 of argv)) with write permission",
			"-e", "write img to f",
			"-e", "close access f",
			"-e", "end run",
			path).Run()
		if err != nil {
			os.Remove(path)
			return pasteMsg{err: errNoImage}
		}
		return pasteMsg{path: path}
	}
}

// pastedPaths returns the files referenced by pasted text, which is what a
// terminal inserts when files are dragged in from Finder. It returns nil
// unless every token is an existing regular file.
func pastedPaths(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" || strings.Contains(s, "\n") {
		return nil
	}
	var tokens []string
	var cur strings.Builder
	quote := rune(0)
	esc := false
	for _, r := range s {
		switch {
		case esc:
			cur.WriteRune(r)
			esc = false
		case r == '\\' && quote != '\'':
			esc = true
		case quote != 0 && r == quote:
			quote = 0
		case quote == 0 && (r == '\'' || r == '"'):
			quote = r
		case quote == 0 && r == ' ':
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}
	home, _ := os.UserHomeDir()
	for i, t := range tokens {
		if strings.HasPrefix(t, "~/") {
			t = filepath.Join(home, t[2:])
		}
		if !filepath.IsAbs(t) {
			return nil
		}
		if fi, err := os.Stat(t); err != nil || !fi.Mode().IsRegular() {
			return nil
		}
		tokens[i] = t
	}
	return tokens
}

func (m *Model) addAttachment(path string, temp bool) {
	if len(m.attach) >= maxFiles {
		m.setStatus(fmt.Sprintf(tr("At most %d files per message"), maxFiles), true)
		if temp {
			os.Remove(path)
		}
		return
	}
	fi, err := os.Stat(path)
	if err != nil {
		m.setStatus(tr("Cannot read file: ")+err.Error(), true)
		return
	}
	name := filepath.Base(path)
	if temp {
		name = fmt.Sprintf("image-%d.png", len(m.attach)+1)
	}
	m.attach = append(m.attach, attachment{path: path, name: name, size: fi.Size(), temp: temp})
}

// Cleanup removes clipboard images that were attached but never sent.
func (m *Model) Cleanup() {
	for _, a := range m.attach {
		if a.temp {
			os.Remove(a.path)
		}
	}
}

func (m *Model) dropAttachment() {
	a := m.attach[len(m.attach)-1]
	if a.temp {
		os.Remove(a.path)
	}
	m.attach = m.attach[:len(m.attach)-1]
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func (m *Model) viewAttachments(w int) string {
	parts := make([]string, 0, len(m.attach))
	for _, a := range m.attach {
		parts = append(parts, stAccent.Render(tr("[file] "))+a.name+stDim.Render(" "+humanSize(a.size)))
	}
	return " " + strings.Join(parts, "  ") + stDim.Render(tr("  · Backspace on an empty input removes"))
}

// uploadAndSend uploads the attachments, then creates the post.
func (m *Model) uploadAndSend(p *model.Post, files []attachment) tea.Cmd {
	fail := func(err error) sentMsg {
		return sentMsg{pending: p.PendingPostId, err: err, channel: p.ChannelId, root: p.RootId, text: p.Message, files: files}
	}
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		for _, a := range files {
			data, err := os.ReadFile(a.path)
			if err != nil {
				return fail(err)
			}
			id, err := m.c.Upload(cx, p.ChannelId, a.name, data)
			if err != nil {
				return fail(err)
			}
			p.FileIds = append(p.FileIds, id)
		}
		created, err := m.c.Send(cx, p)
		if err != nil {
			return fail(err)
		}
		for _, a := range files {
			if a.temp {
				os.Remove(a.path)
			}
		}
		return sentMsg{pending: p.PendingPostId, post: created}
	}
}

// maxDownload caps attachments fetched for opening; larger files are
// better handled in the web app.
const maxDownload = 100 << 20

// safeOpen lists file types opened directly. Anything else (apps, scripts,
// .terminal/.fileloc bundles, installers) is only revealed in the file
// manager so a message can never run code with a single keypress.
var safeOpen = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".heic": true, ".bmp": true,
	".pdf": true, ".txt": true, ".md": true, ".csv": true, ".log": true, ".json": true,
	".mp4": true, ".mov": true, ".mp3": true, ".m4a": true, ".wav": true,
}

// openFilesCmd downloads a post's attachments into a private temp folder and
// opens the safe ones with the default app.
func (m *Model) openFilesCmd(p *model.Post) tea.Cmd {
	var files []*model.FileInfo
	if p.Metadata != nil {
		files = p.Metadata.Files
	}
	if len(files) == 0 || p.Id == "" {
		m.setStatus(tr("This message has no attachments"), false)
		return nil
	}
	m.setStatus(tr("Downloading…"), false)
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		dir, err := os.MkdirTemp("", "mmt-files-")
		if err != nil {
			return openedMsg{err: err}
		}
		for _, f := range files {
			if f.Size > maxDownload {
				return openedMsg{err: fmt.Errorf("%s: %s", f.Name, tr("too large, open it on the web"))}
			}
			data, err := m.c.File(cx, f.Id)
			if err != nil {
				return openedMsg{err: err}
			}
			name := filepath.Base(f.Name)
			if name == "." || name == "/" || name == "" {
				name = "attachment"
			}
			path := filepath.Join(dir, name)
			if err := os.WriteFile(path, data, 0o600); err != nil {
				return openedMsg{err: err}
			}
			markQuarantined(path)
			if safeOpen[strings.ToLower(filepath.Ext(name))] {
				err = openExternal(path, "")
			} else {
				err = revealFile(path)
			}
			if err != nil {
				return openedMsg{err: err}
			}
		}
		return openedMsg{}
	}
}

// markQuarantined tags a downloaded file like a browser would, so macOS
// Gatekeeper still checks it if the user opens it later.
func markQuarantined(path string) {
	if runtime.GOOS != "darwin" {
		return
	}
	v := fmt.Sprintf("0081;%x;mmt;", time.Now().Unix())
	_ = exec.Command("xattr", "-w", "com.apple.quarantine", v, path).Run()
}

// cleanTempFiles removes downloads and pasted images left by earlier runs.
func cleanTempFiles() {
	for _, pattern := range []string{"mmt-files-*", "mmt-paste-*"} {
		matches, _ := filepath.Glob(filepath.Join(os.TempDir(), pattern))
		for _, p := range matches {
			if fi, err := os.Stat(p); err == nil && time.Since(fi.ModTime()) > 24*time.Hour {
				_ = os.RemoveAll(p)
			}
		}
	}
}
