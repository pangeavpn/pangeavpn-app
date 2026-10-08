package procmatch

import (
	"path"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	maxRuleLen     = 1024
	resolveTimeout = 2 * time.Second
)

// ValidateRule checks one stored app entry; "" means it is acceptable.
func ValidateRule(entry string) string {
	_, code := hostEnv().check(entry, nil)
	return code
}

// CompileRules normalises valid entries for this OS and reports the rest by index.
func CompileRules(entries []string) (*Rules, []RuleError) {
	return compileRules(hostEnv(), entries, nil)
}

// CompileRulesProtected also records images whose process trees never bypass; entries
// overlapping them are refused with CodeOwnImage.
func CompileRulesProtected(entries, neverBypass []string) (*Rules, []RuleError) {
	return compileRules(hostEnv(), entries, neverBypass)
}

// ValidateRules reports what CompileRulesProtected would refuse, without the background
// resolution a compile starts (filesystem work as root that a validate-only caller discards).
func ValidateRules(entries, neverBypass []string) []RuleError {
	env := hostEnv()
	_, errs := checkEntries(env, entries, parseImages(env, neverBypass))
	return errs
}

func compileRules(env *ruleEnv, entries, neverBypass []string) (*Rules, []RuleError) {
	never := compileImages(env, neverBypass)
	var neverRules []compiledRule
	if never != nil {
		neverRules = never.rules
	}
	out, errs := checkEntries(env, entries, neverRules)
	rules := newRules(env.goos, out)
	rules.never = never
	rules.translocated = env.translocated
	rules.startResolve(env, true, neverRules)
	return rules, errs
}

func checkEntries(env *ruleEnv, entries []string, never []compiledRule) ([]compiledRule, []RuleError) {
	var errs []RuleError
	var out []compiledRule
	for i, entry := range entries {
		r, code := env.check(entry, never)
		if code != "" {
			errs = append(errs, RuleError{Index: i, Code: code})
			continue
		}
		if slices.Contains(out, r) {
			continue
		}
		if len(out) >= MaxRules {
			errs = append(errs, RuleError{Index: i, Code: CodeTooMany})
			continue
		}
		out = append(out, r)
	}
	return out, errs
}

func parseImages(env *ruleEnv, paths []string) []compiledRule {
	var out []compiledRule
	for _, p := range paths {
		r, code := parseRule(env.goos, p)
		if code == "" && !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	return out
}

// compileImages builds a matcher for protected images without applying the rule policy.
func compileImages(env *ruleEnv, paths []string) *Rules {
	out := parseImages(env, paths)
	if len(out) == 0 {
		return nil
	}
	rules := newRules(env.goos, out)
	rules.translocated = env.translocated
	rules.startResolve(env, false, nil)
	return rules
}

func newRules(goos string, rules []compiledRule) *Rules {
	slices.SortFunc(rules, func(a, b compiledRule) int {
		if c := strings.Compare(a.raw, b.raw); c != 0 {
			return c
		}
		return int(a.kind) - int(b.kind)
	})
	r := &Rules{goos: goos, rules: rules}
	for _, cr := range rules {
		r.idx.add(cr)
	}
	return r
}

func (r *Rules) Empty() bool { return r == nil || len(r.rules) == 0 }

// Equal compares the normalised entries; resolved forms follow from them.
func (r *Rules) Equal(o *Rules) bool {
	if r.Empty() || o.Empty() {
		return r.Empty() && o.Empty()
	}
	return r.goos == o.goos && slices.Equal(r.rules, o.rules) && r.never.sameEntries(o.never)
}

func (r *Rules) sameEntries(o *Rules) bool {
	if r.Empty() || o.Empty() {
		return r.Empty() && o.Empty()
	}
	return slices.Equal(r.rules, o.rules)
}

func (r *Rules) MatchPath(p string) bool {
	if r.Empty() || p == "" {
		return false
	}
	if r.goos == "windows" {
		p = normalizeWinImage(p)
	}
	if r.idx.match(p) {
		return true
	}
	if r.res != nil {
		if extra := r.res.extra.Load(); extra != nil && extra.match(p) {
			return true
		}
	}
	if r.goos == "darwin" {
		return r.matchTranslocated(p)
	}
	return false
}

func (r *Rules) MatchChain(chain []string) bool {
	for _, p := range chain {
		if r.MatchPath(p) {
			return true
		}
	}
	return false
}

// matchTranslocated matches a Gatekeeper-translocated copy of an excluded bundle by name. The
// mount is verified too, since the user's own temp dir could otherwise hold a lookalike tree.
func (r *Rules) matchTranslocated(p string) bool {
	if !strings.HasPrefix(p, "/private/var/folders/") {
		return false
	}
	head, rest, ok := strings.Cut(p, "/AppTranslocation/")
	if !ok {
		return false
	}
	// <id>/d/<Name>.app/<inside the bundle>
	parts := strings.SplitN(rest, "/", 4)
	if len(parts) < 4 || parts[0] == "" || parts[1] != "d" || !strings.HasSuffix(strings.ToLower(parts[2]), ".app") {
		return false
	}
	name := parts[2]
	known := r.idx.hasBundle(name)
	if !known && r.res != nil {
		if extra := r.res.extra.Load(); extra != nil && extra.hasBundle(name) {
			known = true
		}
	}
	return known && r.translocated != nil && r.translocated(head+"/AppTranslocation/"+parts[0]+"/d")
}

// matchIndex answers exact matches by map and prefix matches by scan.
type matchIndex struct {
	exact   map[string]struct{}
	prefix  []string
	bundles []string
}

func (m *matchIndex) add(r compiledRule) {
	if r.kind == RuleFile {
		if m.exact == nil {
			m.exact = make(map[string]struct{})
		}
		m.exact[r.raw] = struct{}{}
		return
	}
	m.prefix = append(m.prefix, r.raw)
	if r.kind == RuleBundle {
		m.bundles = append(m.bundles, path.Base(strings.TrimSuffix(r.raw, "/")))
	}
}

func (m *matchIndex) match(p string) bool {
	if _, ok := m.exact[p]; ok {
		return true
	}
	for _, pre := range m.prefix {
		if strings.HasPrefix(p, pre) {
			return true
		}
	}
	return false
}

func (m *matchIndex) hasBundle(name string) bool {
	return slices.Contains(m.bundles, name)
}

// covers reports whether the rule matches p, a file path or a directory form ending in a separator.
func (r compiledRule) covers(p string) bool {
	if r.kind == RuleFile {
		return r.raw == p
	}
	return strings.HasPrefix(p, r.raw)
}

func overlaps(a, b compiledRule) bool {
	return a.covers(b.raw) || b.covers(a.raw)
}

func parseRule(goos, entry string) (compiledRule, string) {
	switch {
	case strings.IndexByte(entry, 0) >= 0:
		return compiledRule{}, CodeNUL
	case len(entry) > maxRuleLen:
		return compiledRule{}, CodeTooLong
	}
	switch goos {
	case "windows":
		return parseWindowsRule(entry)
	case "darwin":
		return parseDarwinRule(entry)
	default:
		return parseLinuxRule(entry)
	}
}

func parseWindowsRule(entry string) (compiledRule, string) {
	p := strings.ReplaceAll(entry, "/", `\`)
	if strings.HasPrefix(p, `\\`) || strings.HasPrefix(p, `\??\`) {
		return compiledRule{}, CodeUnsupportedForm
	}
	if !isDriveAbs(p) {
		return compiledRule{}, CodeNotAbsolute
	}
	clean := cleanWindows(p)
	if strings.HasSuffix(p, `\`) {
		return compiledRule{RuleDir, withSep(clean, `\`)}, ""
	}
	if len(clean) <= len(`c:\`) || !strings.HasSuffix(clean, ".exe") {
		return compiledRule{}, CodeUnsupportedForm
	}
	return compiledRule{RuleFile, clean}, ""
}

func parseDarwinRule(entry string) (compiledRule, string) {
	if !strings.HasPrefix(entry, "/") {
		return compiledRule{}, CodeNotAbsolute
	}
	clean := path.Clean(entry)
	switch {
	case clean != "/" && strings.HasSuffix(strings.ToLower(clean), ".app"):
		return compiledRule{RuleBundle, clean + "/"}, ""
	case strings.HasSuffix(entry, "/"):
		return compiledRule{RuleDir, withSep(clean, "/")}, ""
	}
	return compiledRule{RuleFile, clean}, ""
}

func parseLinuxRule(entry string) (compiledRule, string) {
	if !strings.HasPrefix(entry, "/") {
		return compiledRule{}, CodeNotAbsolute
	}
	clean := path.Clean(entry)
	if snap := snapDir(clean); snap != "" {
		return compiledRule{RuleDir, snap}, ""
	}
	if strings.HasSuffix(entry, "/") {
		return compiledRule{RuleDir, withSep(clean, "/")}, ""
	}
	return compiledRule{RuleFile, clean}, ""
}

// snapDir rewrites /snap/<name>/<rev>/… and /snap/bin/<name>[.<app>] to /snap/<name>/ so rules survive refreshes.
func snapDir(clean string) string {
	parts := strings.Split(strings.TrimPrefix(clean, "/"), "/")
	if len(parts) < 3 || parts[0] != "snap" {
		return ""
	}
	if parts[1] == "bin" {
		name, _, _ := strings.Cut(parts[2], ".")
		if len(parts) != 3 || name == "" {
			return ""
		}
		return "/snap/" + name + "/"
	}
	return "/snap/" + parts[1] + "/"
}

func isDriveAbs(p string) bool {
	if len(p) < 3 || p[1] != ':' || p[2] != '\\' {
		return false
	}
	c := p[0] | 0x20
	return c >= 'a' && c <= 'z'
}

// cleanWindows lower-cases a drive-absolute path and resolves "." and ".." the way Win32 does,
// including dropping trailing dots and spaces from each component.
func cleanWindows(p string) string {
	p = strings.ToLower(p)
	var parts []string
	for _, c := range strings.Split(p[3:], `\`) {
		switch c {
		case "", ".":
			continue
		case "..":
			if len(parts) > 0 {
				parts = parts[:len(parts)-1]
			}
			continue
		}
		if c = strings.TrimRight(c, ". "); c != "" {
			parts = append(parts, c)
		}
	}
	return p[:3] + strings.Join(parts, `\`)
}

// normalizeWinImage puts a process image path into rule form (lower-case, no \\?\ prefix).
func normalizeWinImage(p string) string {
	p = strings.ReplaceAll(p, "/", `\`)
	if rest, ok := strings.CutPrefix(p, `\\?\UNC\`); ok {
		p = `\\` + rest
	} else if rest, ok := strings.CutPrefix(p, `\\?\`); ok {
		p = rest
	}
	return strings.ToLower(p)
}

func withSep(p, sep string) string {
	if strings.HasSuffix(p, sep) {
		return p
	}
	return p + sep
}

func winBase(p string) string {
	if i := strings.LastIndexByte(p, '\\'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// normalizeResolved turns a resolver result into a rule of the original kind.
func normalizeResolved(goos string, kind RuleKind, p string) (compiledRule, bool) {
	if kind != RuleFile {
		if goos == "windows" {
			p = withSep(p, `\`)
		} else {
			p = withSep(p, "/")
		}
	}
	if goos == "windows" {
		p = normalizeWinImage(p)
	}
	r, code := parseRule(goos, p)
	if code != "" {
		return compiledRule{}, false
	}
	if kind == RuleBundle && r.kind == RuleDir {
		r.kind = RuleBundle
	}
	if r.kind != kind {
		return compiledRule{}, false
	}
	return r, true
}

// resolution publishes the resolved (symlink/junction/vnode) forms once lookups finish.
type resolution struct {
	extra atomic.Pointer[matchIndex]
	done  chan struct{}
}

func (r *Rules) startResolve(env *ruleEnv, applyPolicy bool, never []compiledRule) {
	if env.resolve == nil || len(r.rules) == 0 {
		return
	}
	res := &resolution{done: make(chan struct{})}
	r.res = res
	rules := r.rules
	go func() {
		defer close(res.done)
		found := make([][]string, len(rules))
		var wg sync.WaitGroup
		for i, cr := range rules {
			wg.Add(1)
			go func() {
				defer wg.Done()
				found[i] = resolveWithDeadline(env.resolve, cr, resolveTimeout)
			}()
		}
		wg.Wait()
		var idx matchIndex
		n := 0
		for i, cr := range rules {
			for _, f := range found[i] {
				rr, ok := normalizeResolved(env.goos, cr.kind, f)
				if !ok || rr == cr || (applyPolicy && env.policy(rr, never) != "") {
					continue
				}
				idx.add(rr)
				n++
			}
		}
		if n > 0 {
			res.extra.Store(&idx)
		}
	}()
}

func resolveWithDeadline(resolve func(string, RuleKind) []string, r compiledRule, d time.Duration) []string {
	ch := make(chan []string, 1)
	go func() { ch <- resolve(r.raw, r.kind) }()
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case forms := <-ch:
		return forms
	case <-t.C:
		return nil
	}
}

// resolveDone reports whether background resolution of r and its protected images has finished.
func (r *Rules) resolveDone() bool {
	if r == nil {
		return true
	}
	if r.res != nil {
		select {
		case <-r.res.done:
		default:
			return false
		}
	}
	return r.never.resolveDone()
}

// waitResolved blocks until background resolution finishes (tests).
func (r *Rules) waitResolved() {
	if r == nil {
		return
	}
	if r.res != nil {
		<-r.res.done
	}
	r.never.waitResolved()
}
