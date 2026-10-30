//go:build android

package mobile

// Android's own CLAT already carries IPv4 literals over an IPv6-only network.
func nat64Synthesize(literal string) string { return literal }
