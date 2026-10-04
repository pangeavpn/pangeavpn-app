// Package procmatch attributes tunnel flows to the processes that own them and
// matches those processes against the user's split-tunnel app rules.
package procmatch

import (
	"errors"
	"net/netip"
)

type RuleKind uint8

const (
	RuleFile RuleKind = iota
	RuleDir
	RuleBundle
)

// Rule error codes are stable: the daemon API returns them to the desktop app.
const (
	CodeNotAbsolute     = "notAbsolute"
	CodeTooLong         = "tooLong"
	CodeNUL             = "nul"
	CodeUnsupportedForm = "unsupportedForm"
	CodeSystemProcess   = "systemProcess"
	CodeTooBroad        = "tooBroad"
	CodeOwnImage        = "ownImage"
	CodeTooMany         = "tooMany"
)

// MaxRules caps the compiled app rules; further valid entries get CodeTooMany.
const MaxRules = 256

type RuleError struct {
	Index int
	Code  string
}

// Rules is an immutable compiled rule set.
type Rules struct {
	goos  string
	rules []compiledRule
	idx   matchIndex
	res   *resolution
	never *Rules
}

type compiledRule struct {
	kind RuleKind
	raw  string
}

type FlowID struct {
	Proto  uint8
	App    netip.AddrPort
	Remote netip.AddrPort
}

type Verdict uint8

const (
	VerdictTunnel Verdict = iota
	VerdictBypass
)

type Owner struct {
	PID    int
	Start  int64
	SockID uint64
	Chain  []string
}

type Result struct {
	Verdict Verdict
	Owner   Owner
}

type SocketCheck struct {
	Proto uint8
	App   netip.AddrPort
	Owner Owner
}

// Classifier resolves flow owners from one fresh system snapshot per call.
type Classifier interface {
	Classify(rules *Rules, flows []FlowID) []Result
	Validate(checks []SocketCheck) []bool
	Close() error
}

type Options struct {
	SelfPID     int
	NeverBypass []string
	Logf        func(format string, args ...any)
}

// Stats counts owner lookups; classifiers that keep them implement StatsSource.
type Stats struct {
	Lookups     uint64
	LookupFails uint64
	Fallbacks   uint64
	PathFails   uint64
	Refreshes   uint64
}

type StatsSource interface {
	Stats() Stats
}

// RulesObserver is implemented by classifiers that keep process lineage only while rules are
// non-empty. The owner reports every change in order: Classify may still run with older rules.
type RulesObserver interface {
	ObserveRules(rules *Rules)
}

var ErrUnsupported = errors.New("split tunnelling app exclusion is not supported on this OS")
