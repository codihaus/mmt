//go:build !darwin || !cgo

package lock

import "errors"

func TouchIDAvailable() bool { return false }

func TouchID(string) (bool, error) { return false, errors.New("touch ID is not available") }
