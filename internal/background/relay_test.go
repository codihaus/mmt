//go:build unix

package background

import (
	"os"
	"testing"
	"time"
)

func TestRelay(t *testing.T) {
	// a short path: unix socket paths are limited to ~100 bytes
	home, err := os.MkdirTemp("/tmp", "mmt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(home)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", home)

	if Relay("t", "b", "com.apple.Terminal") {
		t.Fatal("relayed with no agent listening")
	}
	got := make(chan relayMsg, 1)
	go serveRelay(func(title, body, app string) { got <- relayMsg{title, body, app} })
	deadline := time.Now().Add(2 * time.Second)
	for !Relay("Luffy", "hi", "com.apple.Terminal") {
		if time.Now().After(deadline) {
			t.Fatal("agent never accepted the notification")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if m := <-got; m != (relayMsg{"Luffy", "hi", "com.apple.Terminal"}) {
		t.Fatalf("got %+v", m)
	}
}

func TestBundleID(t *testing.T) {
	for id, ok := range map[string]bool{
		"com.apple.Terminal":    true,
		"com.googlecode.iterm2": true,
		"":                      false,
		"Terminal":              false,
		"-a Calculator":         false,
		"com.apple.Terminal;x":  false,
	} {
		if bundleID.MatchString(id) != ok {
			t.Errorf("%q: want %v", id, ok)
		}
	}
}
