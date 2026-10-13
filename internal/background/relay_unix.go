//go:build unix

package background

import (
	"encoding/json"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"time"
)

// An mmt window is not an app bundle, so a notification it posts itself
// cannot be clicked back to its terminal on macOS (osascript's open Script
// Editor). It hands them to the agent over this socket instead.
func relayPath() string { return filepath.Join(filepath.Dir(uiLockPath()), "notify.sock") }

type relayMsg struct {
	Title, Body string
	App         string // bundle id of the terminal to bring back on click
}

// Relay asks the agent to show a notification whose click activates app.
// It reports false when no agent can show it.
func Relay(title, body, app string) bool {
	if Blocked() {
		return false
	}
	c, err := net.DialTimeout("unix", relayPath(), time.Second)
	if err != nil {
		return false
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(time.Second))
	return json.NewEncoder(c).Encode(relayMsg{title, body, app}) == nil
}

// serveRelay shows the notifications mmt windows hand over.
func serveRelay(show func(title, body, app string)) {
	p := relayPath()
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	_ = os.Remove(p)
	l, err := net.Listen("unix", p)
	if err != nil {
		log.Printf("relay: %v", err)
		return
	}
	for {
		c, err := l.Accept()
		if err != nil {
			log.Printf("relay: %v", err)
			return
		}
		go func() {
			defer c.Close()
			_ = c.SetDeadline(time.Now().Add(time.Second))
			var m relayMsg
			if json.NewDecoder(io.LimitReader(c, 16<<10)).Decode(&m) == nil {
				show(m.Title, m.Body, m.App)
			}
		}()
	}
}
