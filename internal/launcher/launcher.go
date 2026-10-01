// Package launcher reopens mmt in a terminal that can pass Cmd shortcuts
// through (iTerm2 or Ghostty), since Terminal.app keeps every Cmd key for
// its own menus.
package launcher

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/codihaus/mmt/internal/i18n"
)

type Kind string

const (
	Auto    Kind = "auto"
	ITerm   Kind = "iterm2"
	Ghostty Kind = "ghostty"
	Current Kind = "current"
)

// Label is the user-facing name of a terminal choice.
func (k Kind) Label() string {
	switch k {
	case ITerm:
		return "iTerm2"
	case Ghostty:
		return "Ghostty"
	case Current:
		return i18n.T("current terminal")
	}
	return i18n.T("automatic (iTerm2 → Ghostty → current)")
}

// CmdKeysEnv marks a session whose terminal maps Cmd shortcuts for mmt.
const CmdKeysEnv = "MMT_CMD_KEYS"

// localeEnv gives mmt a UTF-8 locale that exists on every Mac.
const localeEnv = "LANG=en_US.UTF-8 LC_CTYPE=en_US.UTF-8"

func appPath(name string) string {
	home, _ := os.UserHomeDir()
	for _, dir := range []string{"/Applications", filepath.Join(home, "Applications")} {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func Installed(k Kind) bool {
	switch k {
	case ITerm:
		return appPath("iTerm.app") != ""
	case Ghostty:
		return appPath("Ghostty.app") != ""
	}
	return k == Current
}

// Resolve turns a preference into a terminal that is actually available.
func Resolve(pref Kind) Kind {
	switch pref {
	case ITerm, Ghostty:
		if Installed(pref) {
			return pref
		}
		return Current
	case Current:
		return Current
	}
	if Installed(ITerm) {
		return ITerm
	}
	if Installed(Ghostty) {
		return Ghostty
	}
	return Current
}

func running() Kind {
	switch os.Getenv("TERM_PROGRAM") {
	case "iTerm.app":
		return ITerm
	case "ghostty":
		return Ghostty
	}
	return Current
}

// ShouldRelaunch is true when mmt runs in a plain terminal window while a
// better one is configured. tmux, zellij and SSH sessions are left alone.
func ShouldRelaunch(target Kind) bool {
	if target == Current || running() == target {
		return false
	}
	if os.Getenv("MMT_HERE") != "" || InMultiplexer() || os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "" {
		return false
	}
	return true
}

// InMultiplexer reports a session inside tmux, screen or zellij, which do
// not pass iTerm2 escapes through. MMT_MULTIPLEXER=1 marks others.
func InMultiplexer() bool {
	for _, v := range []string{"TMUX", "STY", "ZELLIJ", "MMT_MULTIPLEXER"} {
		if os.Getenv(v) != "" {
			return true
		}
	}
	return false
}

// Launch opens exe in a new window of the target terminal.
func Launch(target Kind, exe string) error {
	switch target {
	case ITerm:
		return launchITerm(exe)
	case Ghostty:
		return launchGhostty(exe)
	}
	return errors.New(i18n.T("no terminal to open"))
}

// ---- iTerm2 ----

const itermProfile = "mmt"

// iTerm2 key-map entries: "<key code>-<modifier mask>". Action 11 sends hex
// codes, action 10 an escape sequence (without the leading ESC).
func itermKeyMap() map[string]any {
	const cmd, shift, numpad = 0x100000, 0x20000, 0x200000
	hex := func(code string) map[string]any { return map[string]any{"Action": 11, "Text": code} }
	esc := func(seq string) map[string]any { return map[string]any{"Action": 10, "Text": seq} }
	key := func(code, mods int) string { return fmt.Sprintf("0x%x-0x%x", code, mods) }
	return map[string]any{
		key('k', cmd):           hex("0x0b"),  // Cmd+K  -> Ctrl+K quick switcher
		key('v', cmd):           hex("0x16"),  // Cmd+V  -> Ctrl+V paste (image or text)
		key('t', cmd):           hex("0x14"),  // Cmd+T  -> Ctrl+T next team
		key(0xf700, cmd|numpad): esc("[1;3A"), // Cmd+Up   -> Alt+Up
		key(0xf701, cmd|numpad): esc("[1;3B"), // Cmd+Down -> Alt+Down
		key(0xd, shift):         hex("0x0a"),  // Shift+Enter -> newline
		key('a', cmd|shift):     esc("a"),     // Cmd+Shift+A -> Alt+A next unread
		key('A', cmd|shift):     esc("a"),
	}
}

// writeITermProfile installs a Dynamic Profile; iTerm2 picks it up without a
// restart and the user's own profiles stay untouched. Reports whether the
// file changed.
func writeITermProfile() (bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}
	dir := filepath.Join(home, "Library", "Application Support", "iTerm2", "DynamicProfiles")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	data, err := json.MarshalIndent(map[string]any{
		"Profiles": []map[string]any{{
			"Name":             itermProfile,
			"Guid":             "mmt-mattermost-terminal",
			"Keyboard Map":     itermKeyMap(),
			"Option Key Sends": 2, // Esc+, so Option works as Alt
			// macOS regions such as en_VN have no UNIX locale, which makes
			// iTerm2 ask on every new window; mmt sets LANG itself instead
			"Set Local Environment Vars": 0,
		}},
	}, "", "  ")
	if err != nil {
		return false, err
	}
	path := filepath.Join(dir, "mmt.json")
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
		return false, nil
	}
	return true, os.WriteFile(path, data, 0o644)
}

// AdoptITermProfile switches the current iTerm2 session to the mmt profile
// (so Cmd shortcuts work in place) and returns a func that switches back.
// ok is false outside a direct iTerm2 session.
func AdoptITermProfile() (restore func(), ok bool) {
	if os.Getenv("TERM_PROGRAM") != "iTerm.app" || InMultiplexer() {
		return nil, false
	}
	orig := os.Getenv("ITERM_PROFILE")
	switch orig {
	case itermProfile:
		return func() {}, true
	case "":
		// without the current profile name there is nothing to switch back to
		return nil, false
	}
	changed, err := writeITermProfile()
	if err != nil {
		return nil, false
	}
	if changed {
		time.Sleep(1500 * time.Millisecond)
	}
	fmt.Print("\x1b]1337;SetProfile=" + itermProfile + "\a")
	return func() { fmt.Print("\x1b]1337;SetProfile=" + orig + "\a") }, true
}

func launchITerm(exe string) error {
	changed, err := writeITermProfile()
	if err != nil {
		return err
	}
	if changed {
		// give a running iTerm2 time to load the new profile
		time.Sleep(1500 * time.Millisecond)
	}
	// arguments go through argv so the path is never parsed as AppleScript
	return exec.Command("osascript",
		"-e", "on run argv",
		"-e", `tell application "iTerm"`,
		"-e", "activate",
		"-e", `create window with profile "`+itermProfile+`" command (item 1 of argv)`,
		"-e", "end tell",
		"-e", "end run",
		"/usr/bin/env "+CmdKeysEnv+"=1 "+localeEnv+" "+shellQuote(exe)).Run()
}

// shellQuote quotes a path for iTerm2's command string, which is split on
// spaces like a shell command line.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ---- Ghostty ----

func launchGhostty(exe string) error {
	binds := []string{
		`cmd+k=text:\x0b`,
		`cmd+v=text:\x16`,
		`cmd+t=text:\x14`,
		`cmd+up=text:\x1b[1;3A`,
		`cmd+down=text:\x1b[1;3B`,
		`shift+enter=text:\n`,
		`cmd+shift+a=text:\x1ba`,
	}
	args := []string{"-na", appPath("Ghostty.app"), "--args", "--macos-option-as-alt=true"}
	for _, b := range binds {
		args = append(args, "--keybind="+b)
	}
	args = append(args, "-e", "/usr/bin/env", CmdKeysEnv+"=1")
	args = append(args, strings.Fields(localeEnv)...)
	args = append(args, exe)
	return exec.Command("open", args...).Run()
}
