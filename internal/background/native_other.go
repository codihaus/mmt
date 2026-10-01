//go:build !(darwin && cgo)

package background

import (
	"os/exec"
	"runtime"
)

const nativeNotifications = false

// runMain blocks forever. Without Cocoa there is no event loop to run and
// notifications cannot be clicked.
func runMain(func(channel string)) {
	select {}
}

func show(title, body, _ string) {
	if runtime.GOOS != "linux" {
		return
	}
	if _, err := exec.LookPath("notify-send"); err == nil {
		_ = exec.Command("notify-send", "--app-name=mmt", "--", title, body).Run()
	}
}
