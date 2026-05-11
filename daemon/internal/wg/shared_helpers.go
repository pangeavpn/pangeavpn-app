package wg

import (
	"regexp"
	"strings"
)

var tunnelNameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

func sanitizeTunnelName(name string) string {
	cleaned := tunnelNameSanitizer.ReplaceAllString(name, "_")
	if cleaned == "" {
		return "tunnel"
	}
	return cleaned
}

func formatDebugStringList(items []string) string {
	if len(items) == 0 {
		return "(none)"
	}
	return "[" + strings.Join(items, ", ") + "]"
}
