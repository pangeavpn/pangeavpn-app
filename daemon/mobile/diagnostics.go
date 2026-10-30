package mobile

// The anonymous diagnostics report. Ports apps/desktop/src/main/diagnosticsReport.ts
// and diagnosticsUpload.ts; the hub side is hub/src/routes/clientDiagnostics.js.

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

const (
	diagnosticsRoute    = "/api/client/diagnostics"
	diagSectionMaxBytes = 64 << 10
	// diagTotalMaxBytes is half of desktop's: requests here cannot outlast 15s.
	diagTotalMaxBytes = 256 << 10
	// diagMaxInnerBytes keeps the HTML-escaped, base64'd envelope under 1 MiB.
	diagMaxInnerBytes = 700_000
	diagMaxNoteChars  = 500
	diagMaxMetaChars  = 64
	// diagBudgetHalvings bounds the size guard; three always fit 700 kB.
	diagBudgetHalvings = 3
	// diagOmitBelow is capTotal's floor: less room than this omits a section.
	diagOmitBelow = 256
)

var (
	reportCodePattern = regexp.MustCompile(`^[A-Z0-9-]{4,32}$`)
	hubCodePattern    = regexp.MustCompile(`^[A-Z0-9_]{1,64}$`)
	errNoReportCode   = errors.New("DIAGNOSTICS_REJECTED: no usable report code")
)

type diagnosticsSection struct {
	Name  string `json:"name"`
	Bytes int    `json:"bytes"`
	Text  string `json:"text"`
}

type diagnosticsPayload struct {
	AppVersion string               `json:"appVersion"`
	Platform   string               `json:"platform"`
	Arch       string               `json:"arch"`
	OSRelease  string               `json:"osRelease"`
	CreatedAt  string               `json:"createdAt"`
	Note       string               `json:"note,omitempty"`
	Sections   []diagnosticsSection `json:"sections"`
}

// diagnosticsMeta is what describes the sender, none of it identifying.
type diagnosticsMeta struct {
	AppVersion string
	Platform   string
	Arch       string
	OSRelease  string
	CreatedAt  time.Time
}

func newDiagnosticsSection(name, text string) diagnosticsSection {
	return diagnosticsSection{Name: name, Bytes: len(text), Text: text}
}

// formatLogEntries matches desktop's formatDaemonRing, one entry per line.
func formatLogEntries(entries []state.LogEntry) string {
	lines := make([]string, len(entries))
	for i, entry := range entries {
		ts := time.UnixMilli(entry.TS).UTC().Format("2006-01-02T15:04:05.000Z")
		lines[i] = fmt.Sprintf("%s [%s] [%s] %s", ts, entry.Level, entry.Source, entry.Msg)
	}
	return strings.Join(lines, "\n")
}

// tailText keeps the last max bytes, dropping a partial first line so that
// redaction never sees half a secret it can no longer recognise.
func tailText(text string, max int) string {
	size := len(text)
	if size <= max {
		return text
	}
	kept := text[runeStartFrom(text, size-max):]
	if i := strings.IndexByte(kept, '\n'); i >= 0 && i+1 < len(kept) && text[size-len(kept)-1] != '\n' {
		kept = kept[i+1:]
	}
	return fmt.Sprintf("[...truncated, showing last %d bytes of %d]\n%s", len(kept), size, kept)
}

func runeStartFrom(text string, at int) int {
	for at < len(text) && !utf8.RuneStart(text[at]) {
		at++
	}
	return at
}

// capSections ports capTotal: sections fill the budget in order, the first
// that overflows keeps its tail, and once under 256 bytes remain the rest are omitted.
func capSections(sections []diagnosticsSection, budget int) []diagnosticsSection {
	remaining := budget
	out := make([]diagnosticsSection, 0, len(sections))
	for _, section := range sections {
		size := len(section.Text)
		switch {
		case size <= remaining:
			remaining -= size
			out = append(out, newDiagnosticsSection(section.Name, section.Text))
		case remaining < diagOmitBelow:
			remaining = 0
			out = append(out, newDiagnosticsSection(section.Name, "<"+section.Name+" omitted: payload size limit reached>"))
		default:
			kept := section.Text[runeStartFrom(section.Text, size-remaining):]
			remaining = 0
			out = append(out, newDiagnosticsSection(section.Name, "[...truncated to fit the report]\n"+kept))
		}
	}
	return out
}

// normalizeNote trims, keeps the first 500 runes and redacts; "" omits the field.
func normalizeNote(note, home string) string {
	trimmed := strings.TrimFunc(note, isJSSpace)
	if trimmed == "" {
		return ""
	}
	if utf8.RuneCountInString(trimmed) > diagMaxNoteChars {
		trimmed = string([]rune(trimmed)[:diagMaxNoteChars])
	}
	return redactDiagnostics(trimmed, home)
}

// ringSection treats text as untrusted: it is tailed and redacted even when
// its sender already did both.
func ringSection(name, text, home string) diagnosticsSection {
	clean := strings.ToValidUTF8(text, "�")
	if strings.TrimFunc(clean, isJSSpace) == "" {
		return newDiagnosticsSection(name, "<"+name+" empty>")
	}
	return newDiagnosticsSection(name, redactDiagnostics(tailText(clean, diagSectionMaxBytes), home))
}

func metaField(value string) string {
	trimmed := strings.TrimFunc(strings.ToValidUTF8(value, ""), isJSSpace)
	if trimmed == "" {
		return "unknown"
	}
	if utf8.RuneCountInString(trimmed) > diagMaxMetaChars {
		trimmed = string([]rune(trimmed)[:diagMaxMetaChars])
	}
	return trimmed
}

// buildDiagnosticsBody marshals the report, halving the section budget until
// the plain json.Marshal size fits under diagMaxInnerBytes.
func buildDiagnosticsBody(meta diagnosticsMeta, note string, sections []diagnosticsSection, home string) ([]byte, error) {
	payload := diagnosticsPayload{
		AppVersion: metaField(meta.AppVersion),
		Platform:   metaField(meta.Platform),
		Arch:       metaField(meta.Arch),
		OSRelease:  metaField(meta.OSRelease),
		CreatedAt:  meta.CreatedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
		Note:       normalizeNote(note, home),
	}
	budget := diagTotalMaxBytes
	for attempt := 0; attempt <= diagBudgetHalvings; attempt++ {
		payload.Sections = capSections(sections, budget)
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal diagnostics: %w", err)
		}
		if len(body) <= diagMaxInnerBytes {
			return body, nil
		}
		budget /= 2
	}
	return nil, errors.New("DIAGNOSTICS_REJECTED: the report does not fit the hub's size limit")
}

// parseReportCode reads the hub's answer, naming the hub's error code on a
// refusal so the host can tell a rate limit from a rejected report.
func parseReportCode(status int, body []byte) (string, error) {
	if status < 200 || status >= 300 {
		var refusal struct {
			Code string `json:"code"`
		}
		if json.Unmarshal(body, &refusal) == nil && hubCodePattern.MatchString(refusal.Code) {
			return "", fmt.Errorf("DIAGNOSTICS_REJECTED: hub answered %d (%s)", status, refusal.Code)
		}
		return "", fmt.Errorf("DIAGNOSTICS_REJECTED: hub answered %d", status)
	}
	var answer struct {
		ReportCode any `json:"reportCode"`
	}
	if json.Unmarshal(body, &answer) != nil {
		return "", errNoReportCode
	}
	code, _ := answer.ReportCode.(string)
	code = strings.TrimFunc(code, isJSSpace)
	if !reportCodePattern.MatchString(code) {
		return "", errNoReportCode
	}
	return code, nil
}
