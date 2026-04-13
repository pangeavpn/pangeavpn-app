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

func compactDiagnosticOutput(output string, maxLines int, maxChars int) string {
	rawLines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lines = append(lines, trimmed)
		if len(lines) >= maxLines {
			break
		}
	}

	if len(lines) == 0 {
		return ""
	}

	joined := strings.Join(lines, " ; ")
	if len(joined) > maxChars {
		return joined[:maxChars] + "..."
	}
	return joined
}
