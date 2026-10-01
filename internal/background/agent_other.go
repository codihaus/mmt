//go:build !darwin

package background

import "errors"

// The background agent is macOS only for now.

func Supported() bool             { return nativeNotifications }
func Enabled() bool               { return false }
func Running() bool               { return false }
func LogPath() string             { return "" }
func Restart()                    {}
func Refresh(exe, version string) {}
func Disable() error              { return nil }
func Enable(exe, version string) error {
	return errors.New("the background agent is only available on macOS")
}
