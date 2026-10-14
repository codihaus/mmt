// Package sound plays mmt's sounds: the ringtone for an incoming call and the
// buzz. A ring repeats until the call ends, the user joins it, or 30 seconds
// pass, like the Mattermost web app.
//
// ring.wav is one ring of an Iskra Eta 80 rotary telephone, cut from
// "WWS AutomatictelephoneIskraEta80ringing.ogg" by Work With Sounds /
// Technical Museum of Slovenia, CC BY 4.0:
// https://commons.wikimedia.org/wiki/File:WWS_AutomatictelephoneIskraEta80ringing.ogg
// buzz.wav is the BUZZ! of Yahoo! Messenger, a sound owned by Yahoo.
package sound

import (
	"bytes"
	"context"
	_ "embed"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// Off in the config silences a sound.
const Off = "off"

const (
	maxRing = 30 * time.Second
	gap     = time.Second
)

// tone names a sound's system default on each platform.
type tone struct {
	darwin  string // in /System/Library/Sounds
	windows string // in %SystemRoot%\Media
	xdg     string // freedesktop sound theme id
}

var (
	ringTone = tone{"Glass.aiff", "Ring01.wav", "phone-incoming-call"}
	buzzTone = tone{"Funk.aiff", "Windows Exclamation.wav", "dialog-warning"}
)

var (
	//go:embed ring.wav
	ringWAV []byte
	//go:embed buzz.wav
	buzzWAV []byte

	ringFile = sync.OnceValue(func() string { return cached("ring.wav", ringWAV) })
	buzzFile = sync.OnceValue(func() string { return cached("buzz.wav", buzzWAV) })
)

// cached writes a built-in sound to the cache, since the players take a
// file; empty when that fails, and the system sound stands in.
func cached(name string, data []byte) string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	p := filepath.Join(dir, "mmt", name)
	if old, err := os.ReadFile(p); err == nil && bytes.Equal(old, data) {
		return p
	}
	if os.MkdirAll(filepath.Dir(p), 0o700) != nil || os.WriteFile(p, data, 0o600) != nil {
		return ""
	}
	return p
}

var (
	mu      sync.Mutex
	stop    context.CancelFunc
	ringing string // channel of the call that rings
)

// Ring rings for a call in channelID, replacing any ring already going.
// file is a sound to play; empty uses the built-in ring, Off plays nothing.
func Ring(channelID, file string) {
	if file == Off {
		return
	}
	if file == "" {
		file = ringFile()
	}
	mu.Lock()
	defer mu.Unlock()
	if stop != nil {
		stop()
	}
	ctx, cancel := context.WithTimeout(context.Background(), maxRing)
	stop, ringing = cancel, channelID
	go func() {
		defer cancel()
		for ctx.Err() == nil {
			cmd := command(ctx, file, ringTone)
			if cmd == nil {
				return
			}
			// a player that fails (missing file, no audio device) would
			// fail every time, so give up instead of spinning
			if err := cmd.Run(); err != nil && ctx.Err() == nil {
				return
			}
			select {
			case <-ctx.Done():
			case <-time.After(gap):
			}
		}
	}()
}

// StopRing ends the ring for a call in channelID; an empty id ends any ring.
// Canceling kills the player, so the sound stops at once.
func StopRing(channelID string) {
	mu.Lock()
	defer mu.Unlock()
	if stop != nil && (channelID == "" || channelID == ringing) {
		stop()
		stop, ringing = nil, ""
	}
}

// Buzz plays the buzz sound once. file is a sound to play; empty uses the
// built-in buzz, Off plays nothing.
func Buzz(file string) {
	if file == Off {
		return
	}
	if file == "" {
		file = buzzFile()
	}
	if cmd := command(context.Background(), file, buzzTone); cmd != nil {
		go cmd.Run()
	}
}

// command plays file, or t's system sound, once with the platform's player;
// nil when there is none. The file travels as an argument or an environment
// variable, never as script text.
func command(ctx context.Context, file string, t tone) *exec.Cmd {
	switch runtime.GOOS {
	case "darwin":
		if file == "" {
			file = "/System/Library/Sounds/" + t.darwin
		}
		return exec.CommandContext(ctx, "afplay", file)
	case "windows":
		if file == "" {
			file = filepath.Join(os.Getenv("SystemRoot"), "Media", t.windows)
		}
		cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command",
			"(New-Object Media.SoundPlayer $env:MMT_SOUND_FILE).PlaySync()")
		cmd.Env = append(os.Environ(), "MMT_SOUND_FILE="+file)
		return cmd
	}
	if file == "" {
		if _, err := exec.LookPath("canberra-gtk-play"); err == nil {
			return exec.CommandContext(ctx, "canberra-gtk-play", "-i", t.xdg)
		}
		file = "/usr/share/sounds/freedesktop/stereo/" + t.xdg + ".oga"
	}
	for _, p := range []string{"paplay", "pw-play", "aplay"} {
		if _, err := exec.LookPath(p); err == nil {
			return exec.CommandContext(ctx, p, file)
		}
	}
	return nil
}
