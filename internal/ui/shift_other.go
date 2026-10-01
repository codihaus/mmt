//go:build !darwin || !cgo

package ui

func shiftHeld() bool { return false }
