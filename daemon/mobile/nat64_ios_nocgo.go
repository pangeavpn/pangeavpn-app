//go:build ios && !cgo

package mobile

// Without cgo there is no getaddrinfo to ask. Only CI's compile check builds
// this; the shipped framework always links the cgo one.
func nat64Synthesize(literal string) string { return literal }
