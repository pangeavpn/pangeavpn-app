//go:build !linux

package platform

// StrictReversePathFilter only has a meaning on Linux, where rp_filter applies.
func StrictReversePathFilter() (bool, error) {
	return false, nil
}
