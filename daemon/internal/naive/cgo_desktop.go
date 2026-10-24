//go:build naive_cgo && !android

package naive

// Desktop hosts have no VpnService, so nothing needs protecting.
func installSocketProtector() {}
