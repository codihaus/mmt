//go:build unix

// Package instance keeps one mmt window per server: opening mmt again
// closes the window that was already running, the way herdr does.
package instance

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func lockPath(server string) string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	sum := sha256.Sum256([]byte(server))
	return filepath.Join(dir, "mmt", "instance-"+hex.EncodeToString(sum[:6])+".lock")
}

// Claim makes this process the mmt window for server. A window already
// running for that server is asked to quit and given a few seconds to let
// go. onTakeover runs when a newer window later claims the server from
// this one. The returned func gives the claim up.
func Claim(server string, onTakeover func()) (release func()) {
	p := lockPath(server)
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return func() {}
	}
	if !tryLock(f) {
		takeOver(f)
	}
	_ = f.Truncate(0)
	_, _ = f.WriteAt([]byte(strconv.Itoa(os.Getpid())), 0)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGUSR1)
	go func() {
		for range sig {
			onTakeover()
		}
	}()
	return func() {
		signal.Stop(sig)
		f.Close()
	}
}

func tryLock(f *os.File) bool {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil
}

// takeOver asks the running window to quit (SIGUSR1, which it handles by
// closing cleanly), and if it hangs, to terminate (SIGTERM, which still
// restores its terminal).
func takeOver(f *os.File) {
	b := make([]byte, 32)
	n, _ := f.ReadAt(b, 0)
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b[:n])))
	if pid <= 0 || pid == os.Getpid() {
		return
	}
	for _, s := range []syscall.Signal{syscall.SIGUSR1, syscall.SIGTERM} {
		if syscall.Kill(pid, s) != nil {
			break
		}
		for i := 0; i < 30; i++ {
			if tryLock(f) {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	// whatever holds it is gone or not ours; carry on without the claim
	_ = tryLock(f)
}
