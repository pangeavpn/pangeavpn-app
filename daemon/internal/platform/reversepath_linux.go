//go:build linux

package platform

// StrictReversePathFilter reports strict reverse-path filtering, which drops the
// replies to bypass sockets: their source routes into the tunnel without the mark.
func StrictReversePathFilter() (bool, error) {
	return strictReversePathFilterAt("/proc")
}
