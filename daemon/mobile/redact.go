package mobile

// Strips identifying values out of log text bound for the hub. Ports
// apps/desktop/src/main/diagnosticsRedact.ts rule for rule, in the same order.

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// jsSpace is JavaScript's \s; RE2's \s is ASCII only.
const (
	jsSpace  = `\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}`
	space    = `[` + jsSpace + `]`
	nonSpace = `[^` + jsSpace + `]`
)

const (
	redactedUser    = "[redacted:user]"
	redactedAccount = "[redacted:accountNumber]"
	// userMarkerHead is what a user-segment rule sees of its own earlier marker.
	userMarkerHead = "[redacted"
)

const (
	// accountSep is what the hub strips between groups, line breaks aside so a
	// marker never joins two lines.
	accountSep          = `[\t\v\f \x{a0}\x{1680}\x{2000}-\x{200a}\x{202f}\x{205f}\x{3000}\x{feff}-]`
	accountNumberLength = 24
)

// accountGroups is six or more groups of four in any case, as the hub folds
// them. RE2 has no look-ahead, so the match carries the rune after it.
var accountGroups = regexp.MustCompile(
	`[0-9A-Za-z]{4}(?:` + accountSep + `{0,3}[0-9A-Za-z]{4}){5,}(?:[^0-9A-Za-z]|$)`)

// redactAccountRun takes the whole run, so a four-letter word beside a number
// can never shift the cut and leave one of its groups readable.
func redactAccountRun(match string) string {
	run, tail := match, ""
	if last, size := utf8.DecodeLastRuneInString(match); !isASCIIAlnum(last) {
		run, tail = match[:len(match)-size], match[len(match)-size:]
	}
	if !isAccountRun(run) {
		return match
	}
	return redactedAccount + tail
}

// isAccountRun spares prose, often six four-letter words: only the dashed form
// the app shows counts on shape alone, while a spaced or unbroken run needs a digit.
func isAccountRun(run string) bool {
	dashed, unbroken := accountGaps(run)
	hasDigit := strings.ContainsAny(run, "0123456789")
	switch {
	case unbroken:
		// An unbroken run of another length is some other token, as desktop treats it.
		return len(run) == accountNumberLength && hasDigit
	case dashed:
		return true
	}
	return hasDigit
}

// accountGaps reports whether every gap between the run's groups holds a dash,
// and whether it has no gaps at all. Groups are four ASCII bytes each.
func accountGaps(run string) (dashed, unbroken bool) {
	dashed, unbroken = true, true
	for at := 4; at < len(run); at += 4 {
		gap := at
		for at < len(run) && !isASCIIAlnum(rune(run[at])) {
			at++
		}
		if at == gap {
			dashed = false
			continue
		}
		unbroken = false
		if !strings.Contains(run[gap:at], "-") {
			dashed = false
		}
	}
	return dashed, unbroken
}

type redactRule func(string) string

// boundedRule stands in for a JS look-behind and look-ahead around re, which
// RE2 cannot express.
type boundedRule struct {
	re *regexp.Regexp
	// blocked is the negative look-behind class.
	blocked func(rune) bool
	// follows says what may come right after a match; the end of text always may.
	follows func(rune) bool
	repl    func(string) string
}

// apply scans like JS: a position whose previous rune is blocked can never
// start a match, so a rejected candidate skips that whole run in one pass.
func (r boundedRule) apply(s string) string {
	var b strings.Builder
	last, at, changed := 0, 0, false
	for at < len(s) {
		at = r.skipBlocked(s, at)
		loc := r.re.FindStringIndex(s[at:])
		if loc == nil {
			break
		}
		start, end := at+loc[0], at+loc[1]
		if r.blockedAt(s, start) || !r.followsAt(s, end) {
			_, size := utf8.DecodeRuneInString(s[start:])
			at = start + size
			continue
		}
		b.WriteString(s[last:start])
		b.WriteString(r.repl(s[start:end]))
		last, at, changed = end, end, true
	}
	if !changed {
		return s
	}
	b.WriteString(s[last:])
	return b.String()
}

func (r boundedRule) blockedAt(s string, at int) bool {
	if at == 0 || r.blocked == nil {
		return false
	}
	prev, _ := utf8.DecodeLastRuneInString(s[:at])
	return r.blocked(prev)
}

func (r boundedRule) followsAt(s string, at int) bool {
	if at >= len(s) {
		return true
	}
	next, _ := utf8.DecodeRuneInString(s[at:])
	return r.follows(next)
}

func (r boundedRule) skipBlocked(s string, at int) int {
	for at < len(s) && r.blockedAt(s, at) {
		_, size := utf8.DecodeRuneInString(s[at:])
		at += size
	}
	return at
}

func templateRule(pattern, template string) redactRule {
	re := regexp.MustCompile(pattern)
	return func(s string) string { return re.ReplaceAllString(s, template) }
}

// userRule redacts the segment after group 1, leaving an earlier marker alone
// so a second pass over redacted text changes nothing.
func userRule(pattern string) redactRule {
	re := regexp.MustCompile(pattern)
	return func(s string) string {
		return replaceSubmatches(re, s, func(groups []string) string {
			if groups[0][len(groups[1]):] == userMarkerHead {
				return groups[0]
			}
			return groups[1] + redactedUser
		})
	}
}

func replaceSubmatches(re *regexp.Regexp, s string, fn func([]string) string) string {
	matches := re.FindAllStringSubmatchIndex(s, -1)
	if matches == nil {
		return s
	}
	var b strings.Builder
	last := 0
	for _, loc := range matches {
		groups := make([]string, len(loc)/2)
		for i := range groups {
			if loc[2*i] >= 0 {
				groups[i] = s[loc[2*i]:loc[2*i+1]]
			}
		}
		b.WriteString(s[last:loc[0]])
		b.WriteString(fn(groups))
		last = loc[1]
	}
	b.WriteString(s[last:])
	return b.String()
}

func bounded(pattern string, blocked, follows func(rune) bool, repl func(string) string) redactRule {
	return boundedRule{re: regexp.MustCompile(pattern), blocked: blocked, follows: follows, repl: repl}.apply
}

// boundedBothSides is the common shape: the same class may neither precede
// nor follow a match.
func boundedBothSides(pattern string, class func(rune) bool, repl func(string) string) redactRule {
	return bounded(pattern, class, func(r rune) bool { return !class(r) }, repl)
}

func constant(text string) func(string) string {
	return func(string) string { return text }
}

func keepIPv4(match string) string {
	if isKeptIPv4(match) {
		return match
	}
	return "[redacted:ipv4]"
}

func keepIPv6(match string) string {
	if isKeptIPv6(match) {
		return match
	}
	return "[redacted:ipv6]"
}

func keepUnlessKey(match string) string {
	if isLikelyKey(match) {
		return "[redacted:key]"
	}
	return match
}

// redactRules runs the widest shapes first so a narrower rule cannot bite a
// piece out of one and leave the rest readable.
var redactRules = []redactRule{
	userRule(`(?i)((?:[A-Za-z]:|\\{1,2}Device\\{1,2}[^\\` + jsSpace + `"]+)?(?:\\{1,2}|/)(?:Users|Documents and Settings)(?:\\{1,2}|/))[^\\/\r\n"':]+`),
	userRule(`(/home/)[^/` + jsSpace + `"':]+`),
	bounded(`/root`, isRootBlocked, isPathEnd, constant("/"+redactedUser)),
	templateRule(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`, "[redacted:email]"),
	templateRule(`(?i)\bBearer`+space+`+[A-Za-z0-9\-_.=+/]+`, "Bearer [redacted:token]"),
	templateRule(`(?i)((?:x-)?license[-_ ]?key["']?`+space+`*[:=]`+space+`*["']?)[^`+jsSpace+`"',}]+`, "${1}[redacted:licenseKey]"),
	templateRule(`(?i)("(?:password|secret|token|api[_-]?key|license[_-]?key|private[_-]?key|preshared[_-]?key|public[_-]?key|account[_-]?number)"`+space+`*:`+space+`*")[^"]*(")`, "${1}[redacted:secret]${2}"),
	templateRule(`\b((?:Private|Preshared|Public)Key`+space+`*=`+space+`*)`+nonSpace+`+`, "${1}[redacted:key]"),
	templateRule(`(?i)\b[0-9a-f]{64}\b`, "[redacted:token]"),
	templateRule(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`, "[redacted:uuid]"),
	boundedBothSides(`[A-Za-z0-9+/]{24,}={0,2}`, isBase64Rune, keepUnlessKey),
	boundedRule{re: accountGroups, blocked: isASCIIAlnum, follows: anyRune, repl: redactAccountRun}.apply,
	// Desktop's own two rules, so a run spared as prose is never one desktop redacts.
	boundedBothSides(`[0-9A-Z]{4}(?:-[0-9A-Z]{4}){5}`, isAlnumOrDash, constant(redactedAccount)),
	boundedBothSides(`[0-9A-HJKMNP-TV-Z]{24}`, isASCIIAlnum, constant(redactedAccount)),
	boundedBothSides(`[0-9]{16}`, isASCIIAlnum, constant(redactedAccount)),
	boundedBothSides(`(?:[0-9A-Fa-f]{1,4}:){7}[0-9A-Fa-f]{1,4}`, isAlnumOrColon, keepIPv6),
	boundedBothSides(`(?:[0-9A-Fa-f]{1,4})?(?::[0-9A-Fa-f]{1,4}){0,6}::(?:[0-9A-Fa-f]{1,4})?(?::[0-9A-Fa-f]{1,4}){0,6}`, isAlnumOrColon, keepIPv6),
	boundedBothSides(`([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})`, isDigitOrDot, keepIPv4),
}

// redactDiagnostics strips accounts, keys, addresses and user names from text.
func redactDiagnostics(text, homeDir string) string {
	if home := homeDirRule(homeDir); home != nil {
		text = home(text)
	}
	for _, rule := range redactRules {
		text = rule(text)
	}
	return text
}

func currentHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

var (
	driveOnly        = regexp.MustCompile(`^[A-Za-z]:$`)
	driveOrBackslash = regexp.MustCompile(`^[A-Za-z]:|\\`)
)

// homeDirRule catches profiles redirected outside Users/home, in raw,
// JSON-escaped and forward-slash spellings. Never the bare user name.
func homeDirRule(homeDir string) redactRule {
	home := strings.TrimRight(strings.TrimFunc(homeDir, isJSSpace), `\/`)
	if jsLength(home) < 4 || driveOnly.MatchString(home) {
		return nil
	}
	var quoted []string
	seen := map[string]bool{}
	for _, form := range []string{strings.ReplaceAll(home, `\`, `\\`), home, strings.ReplaceAll(home, `\`, "/")} {
		if !seen[form] {
			seen[form] = true
			quoted = append(quoted, regexp.QuoteMeta(form))
		}
	}
	flags := ""
	if driveOrBackslash.MatchString(home) {
		flags = "(?i)"
	}
	re, err := regexp.Compile(flags + "(?:" + strings.Join(quoted, "|") + ")")
	if err != nil {
		return nil
	}
	return boundedRule{re: re, follows: isHomePathEnd, repl: constant(redactedUser)}.apply
}

// isKeptIPv4 keeps loopback, RFC1918, link-local and the unspecified and
// broadcast addresses: they say how the tunnel was wired and identify nobody.
func isKeptIPv4(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 4 {
		return true
	}
	var n [4]int
	for i, part := range parts {
		v, ok := parseOctet(part)
		if !ok {
			return true
		}
		n[i] = v
	}
	switch {
	case n[0] == 127 || n[0] == 10 || n[0] == 0:
		return true
	case n[0] == 172 && n[1] >= 16 && n[1] <= 31:
		return true
	case n[0] == 192 && n[1] == 168:
		return true
	case n[0] == 169 && n[1] == 254:
		return true
	case n == [4]int{255, 255, 255, 255}:
		return true
	}
	return false
}

func parseOctet(part string) (int, bool) {
	if part == "" || strings.TrimLeft(part, "0123456789") != "" {
		return 0, false
	}
	v, err := strconv.Atoi(part)
	if err != nil || v > 255 {
		return 0, false
	}
	return v, true
}

func isKeptIPv6(value string) bool {
	lower := strings.ToLower(value)
	if lower == "::" || lower == "::1" {
		return true
	}
	head, _, _ := strings.Cut(lower, ":")
	end := 0
	for end < len(head) && isHexDigit(head[end]) {
		end++
	}
	if end == 0 {
		return false
	}
	group, err := strconv.ParseUint(head[:end], 16, 64)
	if err != nil {
		return false
	}
	return (group >= 0xfe80 && group <= 0xfebf) || (group >= 0xfc00 && group <= 0xfdff)
}

func isLikelyKey(run string) bool {
	if strings.HasSuffix(run, "=") {
		return len(run) >= 24
	}
	mixed := strings.ContainsAny(run, "abcdefghijklmnopqrstuvwxyz") &&
		strings.ContainsAny(run, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") &&
		strings.ContainsAny(run, "0123456789")
	if len(run) >= 43 {
		return mixed
	}
	return len(run) >= 32 && mixed
}

// jsLength counts UTF-16 code units, which is what String.length measures.
func jsLength(s string) int {
	n := 0
	for _, r := range s {
		n++
		if r > 0xffff {
			n++
		}
	}
	return n
}

func isJSSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func isASCIIAlnum(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
}

func isBase64Rune(r rune) bool  { return isASCIIAlnum(r) || r == '+' || r == '/' || r == '=' }
func isAlnumOrDash(r rune) bool { return isASCIIAlnum(r) || r == '-' }
func anyRune(rune) bool         { return true }
func isAlnumOrColon(r rune) bool {
	return isASCIIAlnum(r) || r == ':'
}
func isDigitOrDot(r rune) bool  { return (r >= '0' && r <= '9') || r == '.' }
func isRootBlocked(r rune) bool { return isASCIIAlnum(r) || r == '_' || r == '.' || r == '-' }

// isPathEnd is the look-ahead class of the /root rule.
func isPathEnd(r rune) bool {
	return isJSSpace(r) || strings.ContainsRune(`/"':,;)]}`, r)
}

func isHomePathEnd(r rune) bool { return r == '\\' || isPathEnd(r) }
