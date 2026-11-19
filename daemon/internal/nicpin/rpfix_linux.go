//go:build linux && !android

package nicpin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Strict rp_filter checks a reply unmarked, into the tunnel table, and drops it; connmark carries
// the mark onto replies and src_valid_mark lets the check use it, with no route anyone can follow.
const (
	rpTable           = "pangeavpn_pin"
	srcValidMarkPath  = "/proc/sys/net/ipv4/conf/all/src_valid_mark"
	rpCommandDeadline = 5 * time.Second
)

var rpRuleset = fmt.Sprintf(`table inet %[1]s {
  chain save {
    type filter hook output priority mangle; policy accept;
    meta mark 0x%[2]x ct mark set meta mark
  }
  chain restore {
    type filter hook prerouting priority mangle; policy accept;
    ct mark 0x%[2]x meta mark set ct mark
  }
}
`, rpTable, pinMark)

// rpIPTables are the mangle rules as chain then match; iptablesArgs puts the table first.
var rpIPTables = [][]string{
	{"OUTPUT", "-m", "mark", "--mark", fmt.Sprintf("0x%x", pinMark), "-m", "comment", "--comment", rpTable, "-j", "CONNMARK", "--save-mark"},
	{"PREROUTING", "-m", "connmark", "--mark", fmt.Sprintf("0x%x", pinMark), "-m", "comment", "--comment", rpTable, "-j", "CONNMARK", "--restore-mark"},
}

func iptablesArgs(op string, rule []string) []string {
	return append([]string{"-w", "5", "-t", "mangle", op}, rule...)
}

// rpFix is the refcounted connmark restore; run is replaced by tests.
type rpFix struct {
	mu       sync.Mutex
	holders  int
	backend  string
	restore  string
	run      func(stdin string, name string, args ...string) error
	readFile func(string) ([]byte, error)
	write    func(string, []byte) error
}

var rp = &rpFix{run: runCmd, readFile: os.ReadFile, write: func(p string, b []byte) error { return os.WriteFile(p, b, 0o644) }}

func runCmd(stdin string, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), rpCommandDeadline)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %s: %w (%s)", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (f *rpFix) acquire() (func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.holders == 0 {
		if err := f.install(); err != nil {
			return nil, err
		}
	}
	f.holders++
	var once sync.Once
	return func() { once.Do(f.release) }, nil
}

func (f *rpFix) install() error {
	// A crashed daemon may have left the table; replace it rather than stack a second copy.
	_ = f.run("", "nft", "delete", "table", "inet", rpTable)
	if err := f.run(rpRuleset, "nft", "-f", "-"); err == nil {
		f.backend = "nft"
	} else {
		for _, rule := range rpIPTables {
			_ = f.run("", "iptables", iptablesArgs("-D", rule)...)
		}
		for i, rule := range rpIPTables {
			if err := f.run("", "iptables", iptablesArgs("-A", rule)...); err != nil {
				for _, added := range rpIPTables[:i] {
					_ = f.run("", "iptables", iptablesArgs("-D", added)...)
				}
				return fmt.Errorf("carry the pin mark onto replies: %w", err)
			}
		}
		f.backend = "iptables"
	}
	prev, err := f.readFile(srcValidMarkPath)
	if err != nil {
		f.remove()
		return fmt.Errorf("read src_valid_mark: %w", err)
	}
	f.restore = strings.TrimSpace(string(prev))
	if f.restore != "1" {
		if err := f.write(srcValidMarkPath, []byte("1")); err != nil {
			f.remove()
			return fmt.Errorf("set src_valid_mark: %w", err)
		}
	}
	return nil
}

func (f *rpFix) release() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.holders--; f.holders > 0 {
		return
	}
	f.remove()
	if f.restore != "" && f.restore != "1" {
		_ = f.write(srcValidMarkPath, []byte(f.restore))
	}
	f.restore = ""
}

func (f *rpFix) remove() {
	switch f.backend {
	case "nft":
		_ = f.run("", "nft", "delete", "table", "inet", rpTable)
	case "iptables":
		for _, rule := range rpIPTables {
			_ = f.run("", "iptables", iptablesArgs("-D", rule)...)
		}
	}
	f.backend = ""
}
