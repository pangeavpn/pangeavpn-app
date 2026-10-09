package mobile

import "strings"

const (
	// dohRecordA is the only answer type trusted as an address; a CNAME in the
	// same list names something further to chase.
	dohRecordA = 1
	// maxDoHResponseBytes bounds a resolver reply; a real one is a few hundred bytes.
	maxDoHResponseBytes = 64 << 10
	maxErrorBodyBytes   = 256
)

type dohAnswer struct {
	Type int    `json:"type"`
	Data string `json:"data"`
}

type dohResponse struct {
	Answer []dohAnswer `json:"Answer"`
}

func pickDoHAddress(answers []dohAnswer) (string, bool) {
	for _, answer := range answers {
		if answer.Type == dohRecordA && isIPv4Literal(answer.Data) {
			return answer.Data, true
		}
	}
	return "", false
}

// clipForError keeps an unauthenticated response body from flooding an error
// that may reach the UI or a shared log.
func clipForError(body []byte) string {
	cleaned := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, string(body))
	if len(cleaned) <= maxErrorBodyBytes {
		return cleaned
	}
	return strings.ToValidUTF8(cleaned[:maxErrorBodyBytes], "") + "…"
}
