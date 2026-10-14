package sound

import (
	"bytes"
	"os"
	"testing"
)

func TestBuiltInSounds(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for name, c := range map[string]struct {
		data []byte
		file func() string
	}{"ring": {ringWAV, ringFile}, "buzz": {buzzWAV, buzzFile}} {
		if !bytes.HasPrefix(c.data, []byte("RIFF")) {
			t.Fatalf("%s.wav is not embedded as a WAV file", name)
		}
		p := c.file()
		data, err := os.ReadFile(p)
		if err != nil || !bytes.Equal(data, c.data) {
			t.Fatalf("%s file = %q, err %v", name, p, err)
		}
	}
}
