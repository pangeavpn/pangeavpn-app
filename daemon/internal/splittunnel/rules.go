package splittunnel

import (
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/splittunnel/procmatch"
)

// matcher re-checks owner chains stored on live flows when the rules change.
type matcher interface {
	Empty() bool
	MatchChain(chain []string) bool
	sameRules(o matcher) bool
}

type procRules struct{ *procmatch.Rules }

func (p procRules) sameRules(o matcher) bool {
	q, ok := o.(procRules)
	return ok && p.Rules.Equal(q.Rules)
}

// ruleSet is one immutable generation of app rules; its pointer identifies the generation.
type ruleSet struct {
	rules *procmatch.Rules
	match matcher
	apps  int
}

func (rs *ruleSet) empty() bool {
	return rs == nil || rs.match == nil || rs.match.Empty()
}

func (rs *ruleSet) allows(chain []string) bool {
	return !rs.empty() && len(chain) > 0 && rs.match.MatchChain(chain)
}

func (rs *ruleSet) sameAs(o *ruleSet) bool {
	if rs.empty() || o.empty() {
		return rs.empty() && o.empty()
	}
	return rs.match.sameRules(o.match)
}

func compileRuleSet(apps, never []string) (*ruleSet, []procmatch.RuleError) {
	rules, errs := procmatch.CompileRulesProtected(apps, never)
	return &ruleSet{rules: rules, match: procRules{rules}, apps: len(apps) - len(errs)}, errs
}
