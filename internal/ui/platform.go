package ui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/atotto/clipboard"

	"github.com/codihaus/mmt/internal/launcher"
)

// openExternal opens a URL or file with the desktop's handler. app, when
// set, names the macOS application to use (e.g. "Google Chrome").
func openExternal(target, app string) error {
	switch runtime.GOOS {
	case "darwin":
		args := []string{target}
		if app != "" {
			args = []string{"-a", app, target}
		}
		return exec.Command("open", args...).Run()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", target).Run()
	}
	if app != "" {
		return exec.Command(app, target).Start()
	}
	return exec.Command("xdg-open", target).Run()
}

// revealFile shows a file in the file manager without opening it.
func revealFile(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", "-R", path).Run()
	case "windows":
		return exec.Command("explorer", "/select,", path).Run()
	}
	return exec.Command("xdg-open", filepath.Dir(path)).Run()
}

// copyText puts text on the system clipboard.
func copyText(text string) error {
	var candidates [][]string
	switch runtime.GOOS {
	case "darwin":
		candidates = [][]string{{"pbcopy"}}
	case "windows":
		// clip.exe reads the console code page and mangles UTF-8
		return clipboard.WriteAll(text)
	default:
		candidates = [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}}
	}
	for _, c := range candidates {
		if _, err := exec.LookPath(c[0]); err != nil {
			continue
		}
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Stdin = strings.NewReader(text)
		return cmd.Run()
	}
	return errors.New("no clipboard tool found (install wl-copy or xclip)")
}

// termNotifies reports a terminal that shows notifications for OSC 9 and
// focuses the right window and tab when one is clicked.
var termNotifies = (os.Getenv("TERM_PROGRAM") == "iTerm.app" || os.Getenv("TERM_PROGRAM") == "ghostty") &&
	!launcher.InMultiplexer()

// notify shows a desktop notification. In iTerm2 and Ghostty it goes through
// the terminal (OSC 9), so clicking it brings back the mmt tab; elsewhere it
// uses terminal-notifier when installed (clicking activates the terminal app)
// or osascript. Arguments are passed as argv, never as script text.
func (m *Model) notify(title, body string) {
	title, body = m.noticeText(title, body)
	if r := []rune(body); len(r) > 200 {
		body = string(r[:200]) + "…"
	}
	if termNotifies {
		// os.File serializes writes, so this never lands inside a frame the
		// renderer is writing; OSC 9 does not move the cursor
		text := strings.NewReplacer("\n", " ", "\r", " ").Replace(title + ": " + body)
		_, _ = os.Stdout.WriteString("\x1b]9;" + text + "\a")
		return
	}
	switch runtime.GOOS {
	case "darwin":
		if _, err := exec.LookPath("terminal-notifier"); err == nil {
			args := []string{"-title", "mmt", "-subtitle", title, "-message", body, "-group", "mmt"}
			if id := os.Getenv("__CFBundleIdentifier"); id != "" {
				args = append(args, "-activate", id)
			}
			go exec.Command("terminal-notifier", args...).Run()
			return
		}
		go exec.Command("osascript",
			"-e", "on run argv",
			"-e", "display notification (item 2 of argv) with title (item 1 of argv)",
			"-e", "end run",
			title, body).Run()
	case "linux":
		if _, err := exec.LookPath("notify-send"); err == nil {
			go exec.Command("notify-send", "--", title, body).Run()
		}
	case "windows":
		// a toast through PowerShell; the text travels in environment
		// variables so it is never parsed as script
		cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", windowsToast)
		cmd.Env = append(os.Environ(), "MMT_TOAST_TITLE="+title, "MMT_TOAST_BODY="+body)
		go cmd.Run()
	}
}

const windowsToast = `[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
$xml = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02)
$text = $xml.GetElementsByTagName('text')
$text.Item(0).AppendChild($xml.CreateTextNode($env:MMT_TOAST_TITLE)) | Out-Null
$text.Item(1).AppendChild($xml.CreateTextNode($env:MMT_TOAST_BODY)) | Out-Null
$app = '{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe'
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier($app).Show([Windows.UI.Notifications.ToastNotification]::new($xml))`
