//go:build with_utls

package anytls

// Release builds carry uTLS (scripts/build-daemon.mjs); a plain `go test` does not
// and must not request a fingerprint it cannot produce.
const utlsAvailable = true
