//go:build darwin && cgo

package background

// The Objective-C side lives in native_darwin.go; a file with //export may
// only declare C functions, not define them.

import "C"

import (
	"log"
	"os"
)

var onClick func(channel, url string)

//export mmtClicked
func mmtClicked(channel, url *C.char) {
	if onClick != nil {
		go onClick(C.GoString(channel), C.GoString(url))
	}
}

//export mmtNotifyFailed
func mmtNotifyFailed(msg *C.char) {
	log.Printf("notification: %s", C.GoString(msg))
}

//export mmtPermission
func mmtPermission(status C.int) {
	savePermission(int(status))
}

// savePermission records whether macOS lets the agent show notifications,
// for `mmt background status`.
func savePermission(status int) {
	_ = os.WriteFile(permissionPath(), []byte{byte('0' + status)}, 0o600)
}
