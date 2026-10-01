//go:build darwin && cgo

package ui

/*
#cgo LDFLAGS: -framework ApplicationServices
#include <ApplicationServices/ApplicationServices.h>

static int shiftDown(void) {
	return (CGEventSourceFlagsState(kCGEventSourceStateCombinedSessionState) & kCGEventFlagMaskShift) != 0;
}
*/
import "C"

import "os"

// Terminal.app sends the same byte for Enter and Shift+Enter, so ask the
// window server whether Shift is held at the moment Enter arrives. This only
// makes sense when the keyboard is attached to this machine, not over SSH.
var localKeyboard = os.Getenv("SSH_CONNECTION") == "" && os.Getenv("SSH_TTY") == ""

func shiftHeld() bool {
	return localKeyboard && C.shiftDown() != 0
}
