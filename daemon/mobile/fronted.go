package mobile

// Edge relays that forward the secure envelope to the hub. Ports
// apps/desktop/src/shared/frontedEndpoints.ts.

import (
	"regexp"
	"strings"
)

const maxHostnameLength = 253

// defaultFrontedEndpoints are shipped so an install that has never reached the
// hub has a relay at all; the hub's list replaces them once one arrives.
var defaultFrontedEndpoints = []string{
	"cdn.pangeavpn.it",
	"pangea-relay-org.purple-field-fb05.workers.dev",
	"pangea-relay-alt.purple-field-fb05.workers.dev",
}

var (
	hostLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	// ipv4Like catches dotted quads, which would otherwise pass as four labels.
	ipv4Like = regexp.MustCompile(`^\d{1,3}(\.\d{1,3}){3}$`)
)

// normalizeFrontedEndpoint keeps only the host: the relay answers on 443, and a
// scheme or path from disk would aim the client somewhere unvalidated.
func normalizeFrontedEndpoint(value string) (string, bool) {
	host := strings.ToLower(strings.TrimSpace(value))
	if host == "" || len(host) > maxHostnameLength || ipv4Like.MatchString(host) {
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

// seedFrontedEndpoints is the stored list, or the shipped relays when nothing
// usable is stored.
func seedFrontedEndpoints(stored []string) []string {
	if restored := restoreFrontedEndpoints(stored); len(restored) > 0 {
		return restored
	}
	return append([]string(nil), defaultFrontedEndpoints...)
}

// mergeFrontedEndpoints takes every relay the hub named, or nil when nothing
// changed. An empty advertisement is likelier a rollback, so the cache stays.
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
