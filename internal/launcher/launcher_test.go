package launcher

import (
	"regexp"
	"testing"
)

func TestITermKeyMapFormat(t *testing.T) {
	re := regexp.MustCompile(`^0x[0-9a-f]+-0x[0-9a-f]+$`)
	km := itermKeyMap()
	for k := range km {
		if !re.MatchString(k) {
			t.Errorf("bad iTerm2 key-map entry %q", k)
		}
	}
	if _, ok := km["0x6b-0x100000"]; !ok {
		t.Error("Cmd+K mapping missing")
	}
}

func TestShouldRelaunchSkipsMultiplexers(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")
	t.Setenv("TMUX", "/tmp/tmux-1/default,1,0")
	if ShouldRelaunch(ITerm) {
		t.Error("should not relaunch inside tmux")
	}
	t.Setenv("TMUX", "")
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "")
	t.Setenv("MMT_HERE", "")
	t.Setenv("ZELLIJ", "")
	if !ShouldRelaunch(ITerm) {
		t.Error("should relaunch from Terminal.app into iTerm2")
	}
	if ShouldRelaunch(Current) {
		t.Error("current terminal never relaunches")
	}
}
