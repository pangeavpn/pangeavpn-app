package mobile

// Edge relays that forward the secure envelope to the hub. Ports
// apps/desktop/src/shared/frontedEndpoints.ts.

import (
	"regexp"
	"strings"
)

const maxHostnameLength = 253

var hostLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// normalizeFrontedEndpoint keeps only the host: the relay always answers on
// 443 at /v1/secure, and taking a scheme or path from disk would aim the
// client somewhere this validation cannot reason about.
func normalizeFrontedEndpoint(value string) (string, bool) {
	host := strings.ToLower(strings.TrimSpace(value))
	if host == "" || len(host) > maxHostnameLength {
		return "", false
	}
	labels := strings.Split(host, ".")
	// A bare label could be claimed by an attacker's local search domain.
	if len(labels) < 2 {
		return "", false
	}
	for _, label := range labels {
		if !hostLabel.MatchString(label) {
			return "", false
		}
	}
	return host, true
}

// restoreFrontedEndpoints validates and deduplicates, preserving order.
func restoreFrontedEndpoints(stored []string) []string {
	out := make([]string, 0, len(stored))
	for _, candidate := range stored {
		host, ok := normalizeFrontedEndpoint(candidate)
		if !ok || containsString(out, host) {
			continue
		}
		out = append(out, host)
	}
	return out
}

// mergeFrontedEndpoints takes every relay the hub named, returning nil when
// nothing changed. An empty advertisement leaves the cache alone: a rollback
// is likelier than an instruction to discard the last working addresses.
func mergeFrontedEndpoints(current, advertised []string) []string {
	next := restoreFrontedEndpoints(advertised)
	if len(next) == 0 {
		return nil
	}
	// Keep the relay that last worked in front when the hub still lists it.
	if len(current) > 0 {
		if at := indexOfString(next, current[0]); at > 0 {
			next = moveToFront(next, at)
		}
	}
	if sameStrings(next, current) {
		return nil
	}
	return next
}

// promoteFrontedEndpoint moves the relay that just worked to the front so the
// next start skips the dead ones.
func promoteFrontedEndpoint(list []string, index int) []string {
	if index <= 0 || index >= len(list) {
		return nil
	}
	return moveToFront(append([]string(nil), list...), index)
}

func moveToFront(list []string, index int) []string {
	item := list[index]
	rest := append(list[:index:index], list[index+1:]...)
	return append([]string{item}, rest...)
}

func containsString(list []string, value string) bool {
	return indexOfString(list, value) >= 0
}

func indexOfString(list []string, value string) int {
	for i, item := range list {
		if item == value {
			return i
		}
	}
	return -1
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
