//go:build !unix

package background

func HoldUI() (release func()) { return func() {} }

func uiOpen() bool { return false }

func Blocked() bool { return false }
