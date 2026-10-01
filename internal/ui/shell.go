package ui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/codihaus/mmt/internal/mm"
)

// "!command" runs a shell command on this computer, like the ! prefix in
// other terminal tools. The output is shown in a popup and only reaches the
// chat if you press Enter there. Commands get no keyboard (stdin is empty),
// so interactive programs such as vim or ssh do not fit here.

const (
	shellTimeout = 60 * time.Second
	shellMaxOut  = 64 << 10
	postMaxRunes = 16000 // stay under Mattermost's 16383-character limit
)

type shellView struct {
	cmd     string
	out     string
	code    int
	dur     time.Duration
	err     string
	running bool
	cancel  context.CancelFunc
}

type shellDoneMsg struct {
	out  string
	code int
	dur  time.Duration
	err  error
}

// capped keeps the first shellMaxOut bytes of output.
type capped struct {
	bytes.Buffer
	cut bool
}

func (c *capped) Write(p []byte) (int, error) {
	if room := shellMaxOut - c.Len(); room < len(p) {
		c.cut = true
		if room > 0 {
			c.Buffer.Write(p[:room])
		}
		return len(p), nil
	}
	return c.Buffer.Write(p)
}

func (m *Model) runShell(cmdline string) tea.Cmd {
	cmdline = strings.TrimSpace(cmdline)
	if cmdline == "" {
		return nil
	}
	cx, cancel := context.WithTimeout(context.Background(), shellTimeout)
	m.shell = &shellView{cmd: cmdline, running: true, cancel: cancel}
	m.input.Blur()
	return func() tea.Msg {
		defer cancel()
		var c *exec.Cmd
		if runtime.GOOS == "windows" {
			c = exec.CommandContext(cx, "cmd", "/C", cmdline)
		} else {
			sh := os.Getenv("SHELL")
			if sh == "" {
				sh = "/bin/sh"
			}
			// a login shell, so PATH matches the user's terminal
			c = exec.CommandContext(cx, sh, "-lc", cmdline)
		}
		var out capped
		c.Stdout, c.Stderr = &out, &out
		c.WaitDelay = 2 * time.Second
		start := time.Now()
		err := c.Run()
		code := 0
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code, err = exit.ExitCode(), nil
		}
		switch {
		case errors.Is(cx.Err(), context.DeadlineExceeded):
			err = fmt.Errorf(tr("stopped after %d seconds"), int(shellTimeout.Seconds()))
		case errors.Is(cx.Err(), context.Canceled):
			err = errors.New(tr("cancelled"))
		}
		text := mm.Clean(ansi.Strip(out.String()))
		if out.cut {
			text += "\n" + tr("… output cut at 64 KB")
		}
		return shellDoneMsg{out: strings.TrimRight(text, "\n"), code: code, dur: time.Since(start), err: err}
	}
}

func (m *Model) shellKey(k tea.KeyMsg) tea.Cmd {
	sh := m.shell
	switch k.String() {
	case "esc", "q", "ctrl+c":
		if sh.running {
			sh.cancel()
			return nil
		}
		m.shell = nil
		return m.input.Focus()
	case "c":
		if !sh.running {
			return copyCmd(sh.out)
		}
	case "enter":
		if sh.running {
			return nil
		}
		m.shell = nil
		return tea.Batch(m.postMessage(shellMessage(sh.cmd, sh.out), nil), m.input.Focus())
	}
	return nil
}

// shellMessage formats a command and its output as a code block.
func shellMessage(cmd, out string) string {
	fence := "```"
	for strings.Contains(out, fence) {
		fence += "`"
	}
	body := "$ " + cmd + "\n" + out
	if r := []rune(body); len(r) > postMaxRunes {
		body = string(r[:postMaxRunes]) + "\n…"
	}
	return fence + "\n" + body + "\n" + fence
}

func (m *Model) viewShell(w, h int) string {
	sh := m.shell
	bw := min(100, w-4)
	inner := bw - 4
	var lines []string
	for _, l := range strings.Split(sh.out, "\n") {
		lines = append(lines, strings.Split(ansi.Wrap(strings.ReplaceAll(l, "\t", "    "), inner, ""), "\n")...)
	}
	// keep the end of long output; c copies all of it
	room := max(h-12, 5)
	if len(lines) > room {
		hidden := len(lines) - room
		lines = append([]string{stDim.Render(fmt.Sprintf(tr("… %d lines above (c copies everything)"), hidden))}, lines[hidden:]...)
	}
	if sh.out == "" && !sh.running {
		lines = []string{stDim.Render(tr("(no output)"))}
	}
	var status, hint string
	switch {
	case sh.running:
		status = stAccent.Render(tr("Running…"))
		hint = tr("Esc to stop")
	default:
		st := stOK
		if sh.code != 0 || sh.err != "" {
			st = stErr
		}
		status = st.Render(fmt.Sprintf(tr("exit %d · %s"), sh.code, sh.dur.Round(10*time.Millisecond)))
		if sh.err != "" {
			status += "  " + stErr.Render(sh.err)
		}
		hint = tr("Enter sends the output to the chat · c copy · Esc close")
	}
	title := stTitle.Render("$ ") + ansi.Truncate(sh.cmd, inner-2, "…")
	body := strings.Join(lines, "\n")
	if sh.running && sh.out == "" {
		body = ""
	}
	return stPopup.Width(bw - 2).Render(title + "\n\n" + body + "\n\n" + status + "\n" + stDim.Render(hint))
}
