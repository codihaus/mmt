//go:build !unix

package instance

// Claim is a no-op where there is no flock; every window runs on its own.
func Claim(string, func()) (release func()) { return func() {} }
