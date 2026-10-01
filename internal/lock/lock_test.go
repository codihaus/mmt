package lock

import (
	"testing"

	"github.com/zalando/go-keyring"
)

func TestPasscode(t *testing.T) {
	keyring.MockInit() // never touch the real keychain in tests
	if HasPasscode() {
		t.Fatal("fresh keychain should have no passcode")
	}
	if err := SetPasscode("abc"); err == nil {
		t.Error("too-short passcode accepted")
	}
	if err := SetPasscode("mật khẩu 123"); err != nil {
		t.Fatal(err)
	}
	if ok, err := Verify("mật khẩu 123"); err != nil || !ok {
		t.Errorf("right passcode rejected: %v %v", ok, err)
	}
	if ok, _ := Verify("mat khau 123"); ok {
		t.Error("wrong passcode accepted")
	}
	stored, _ := keyring.Get(keyringService, keyringUser)
	if stored == "" || stored == "mật khẩu 123" {
		t.Errorf("passcode not stored as a hash: %q", stored)
	}
	if err := ClearPasscode(); err != nil || HasPasscode() {
		t.Errorf("clear failed: %v", err)
	}
}
