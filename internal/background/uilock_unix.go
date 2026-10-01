//go:build unix

package background

import (
	"os"
	"path/filepath"
	"syscall"
)

// Every running mmt window holds a shared lock on this file. The background
// agent stays quiet while anyone holds it, so a message is never announced
// twice. The kernel drops the lock when mmt exits, even after a crash.
func uiLockPath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "mmt", "ui.lock")
}

// HoldUI marks mmt as open until the returned func is called.
func HoldUI() (release func()) {
	p := uiLockPath()
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDONLY, 0o600)
	if err != nil {
		return func() {}
	}
	if syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB) != nil {
		f.Close()
		return func() {}
	}
	return func() { f.Close() }
}

// uiOpen reports whether an mmt window is running.
func uiOpen() bool {
	f, err := os.Open(uiLockPath())
	if err != nil {
		return false
	}
	defer f.Close()
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return true
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false
}

// permDenied is UNAuthorizationStatusDenied.
const permDenied = 1

func permissionPath() string { return filepath.Join(filepath.Dir(uiLockPath()), "notifications") }

// Blocked reports that macOS refused the agent's notifications the last
// time it asked.
func Blocked() bool {
	b, err := os.ReadFile(permissionPath())
	return err == nil && len(b) == 1 && int(b[0]-'0') == permDenied
}
