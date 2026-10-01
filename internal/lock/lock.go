// Package lock is mmt's own app lock, separate from the Mattermost login:
// Touch ID where available, and a passcode that is stored only as a PBKDF2
// hash in the system keychain.
package lock

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/zalando/go-keyring"
)

const (
	keyringService = "mmt-lock"
	keyringUser    = "passcode"
	iterations     = 600_000 // OWASP 2023 guidance for PBKDF2-SHA256
	MinLength      = 4
)

// Modes stored in the config.
const (
	ModeOff      = ""
	ModeTouchID  = "touchid"  // Touch ID, passcode as fallback
	ModePasscode = "passcode" // passcode only
)

func hash(passcode string, salt []byte, iter int) ([]byte, error) {
	return pbkdf2.Key(sha256.New, passcode, salt, iter, 32)
}

// SetPasscode stores a new passcode hash.
func SetPasscode(passcode string) error {
	if len([]rune(passcode)) < MinLength {
		return fmt.Errorf("passcode must have at least %d characters", MinLength)
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	h, err := hash(passcode, salt, iterations)
	if err != nil {
		return err
	}
	enc := base64.RawStdEncoding
	return keyring.Set(keyringService, keyringUser,
		fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", iterations, enc.EncodeToString(salt), enc.EncodeToString(h)))
}

func HasPasscode() bool {
	v, err := keyring.Get(keyringService, keyringUser)
	return err == nil && v != ""
}

// Verify checks a passcode against the stored hash in constant time.
func Verify(passcode string) (bool, error) {
	stored, err := keyring.Get(keyringService, keyringUser)
	if err != nil {
		return false, err
	}
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false, errors.New("unknown passcode format")
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil {
		return false, err
	}
	enc := base64.RawStdEncoding
	salt, err := enc.DecodeString(parts[2])
	if err != nil {
		return false, err
	}
	want, err := enc.DecodeString(parts[3])
	if err != nil {
		return false, err
	}
	got, err := hash(passcode, salt, iter)
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

func ClearPasscode() error {
	err := keyring.Delete(keyringService, keyringUser)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
