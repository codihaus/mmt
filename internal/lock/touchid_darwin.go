//go:build darwin && cgo

package lock

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework LocalAuthentication -framework Foundation
#import <LocalAuthentication/LocalAuthentication.h>

static int mmtCanTouchID(void) {
	LAContext *ctx = [[LAContext alloc] init];
	return [ctx canEvaluatePolicy:LAPolicyDeviceOwnerAuthenticationWithBiometrics error:nil] ? 1 : 0;
}

// mmtTouchID shows the system Touch ID prompt and waits for the answer:
// 1 = matched, 0 = failed or cancelled, -1 = Touch ID unavailable.
static int mmtTouchID(const char *reason) {
	LAContext *ctx = [[LAContext alloc] init];
	ctx.localizedFallbackTitle = @""; // the fallback is mmt's own passcode
	if (![ctx canEvaluatePolicy:LAPolicyDeviceOwnerAuthenticationWithBiometrics error:nil]) {
		return -1;
	}
	__block int result = 0;
	dispatch_semaphore_t done = dispatch_semaphore_create(0);
	[ctx evaluatePolicy:LAPolicyDeviceOwnerAuthenticationWithBiometrics
	    localizedReason:[NSString stringWithUTF8String:reason]
	              reply:^(BOOL ok, NSError *err) {
		result = ok ? 1 : 0;
		dispatch_semaphore_signal(done);
	}];
	dispatch_semaphore_wait(done, DISPATCH_TIME_FOREVER);
	return result;
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

func TouchIDAvailable() bool { return C.mmtCanTouchID() == 1 }

// TouchID blocks until the user answers the Touch ID prompt.
func TouchID(reason string) (bool, error) {
	cs := C.CString(reason)
	defer C.free(unsafe.Pointer(cs))
	switch C.mmtTouchID(cs) {
	case 1:
		return true, nil
	case -1:
		return false, errors.New("touch ID is not available")
	}
	return false, nil
}
