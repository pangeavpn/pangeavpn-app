//go:build with_utls

package anytls

// utlsAvailable reports whether sing-box was built with its uTLS layer, which
// release builds always are (see scripts/build-daemon.mjs); a plain
// `go test ./...` is not, and must not request a fingerprint it cannot produce.
const utlsAvailable = true
