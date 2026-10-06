//go:build !with_utls

package anytls

// utlsAvailable is false without -tags with_utls: the outbound then presents
// Go's own ClientHello instead of the browser fingerprint release builds use.
const utlsAvailable = false
