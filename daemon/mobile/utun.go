package mobile

import "strings"

func isUtunName(name string) bool {
	digits, ok := strings.CutPrefix(name, "utun")
	if !ok || digits == "" {
		return false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
