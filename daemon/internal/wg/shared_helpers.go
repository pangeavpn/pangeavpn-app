package wg

import (
	"fmt"
	"hash/fnv"
	"regexp"
	"strings"
)

var tunnelNameSanitizer = regexp.MustCompile(`[^a-z0-9_-]`)

// sanitizeTunnelName derives the internal session map key for a tunnel name.
// Lowercasing makes it case-insensitive to match the profile-name dedupe in
// api/service.go, and the hash suffix keeps names that sanitize to the same
// stem (e.g. "pangea (uk)" vs "pangea [uk]") from colliding.
func sanitizeTunnelName(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	cleaned := tunnelNameSanitizer.ReplaceAllString(lower, "_")
	if cleaned == "" {
		cleaned = "tunnel"
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(lower))
	return fmt.Sprintf("%s_%08x", cleaned, h.Sum32())
}

func formatDebugStringList(items []string) string {
	if len(items) == 0 {
		return "(none)"
	}
	return "[" + strings.Join(items, ", ") + "]"
}
