//go:build linux && !android

package nicpin

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeHost struct {
	calls    []string
	nftFails bool
	sysctl   string
}

func stubRP(t *testing.T, h *fakeHost, strict bool) {
	t.Helper()
	origRP, origStrict := rp, strictReversePath
	t.Cleanup(func() { rp, strictReversePath = origRP, origStrict })
	strictReversePath = func() (bool, error) { return strict, nil }
	rp = &rpFix{
		run: func(stdin, name string, args ...string) error {
			call := name + " " + strings.Join(args, " ")
			h.calls = append(h.calls, call)
			if name == "nft" && h.nftFails {
				return errors.New("no nft")
			}
			return nil
		},
		readFile: func(string) ([]byte, error) { return []byte(h.sysctl + "\n"), nil },
		write:    func(_ string, b []byte) error { h.sysctl = string(b); return nil },
	}
}

func (h *fakeHost) saw(prefix string) bool {
	for _, c := range h.calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func TestAcquire_LinuxLooseReversePathTouchesNothing(t *testing.T) {
	h := &fakeHost{sysctl: "0"}
	stubRP(t, h, false)
	pin, err := Acquire(context.Background(), "104.21.24.200")
	if err != nil || pin.Mark != pinMark || pin.Server != "104.21.24.200" {
		t.Fatalf("Acquire() = %+v, %v", pin, err)
	}
	pin.Release()
	if len(h.calls) != 0 || h.sysctl != "0" {
		t.Fatalf("a loose host was changed: calls %v, src_valid_mark %q", h.calls, h.sysctl)
	}
}

// Strict rp_filter: the connmark restore and src_valid_mark come with the first pin and go
// with the last, the sysctl back to what it was.
func TestAcquire_LinuxStrictReversePathCarriesTheMarkOntoReplies(t *testing.T) {
	for _, nftFails := range []bool{false, true} {
		h := &fakeHost{sysctl: "0", nftFails: nftFails}
		stubRP(t, h, true)
		first, err := Acquire(context.Background(), "104.21.24.200")
		if err != nil {
			t.Fatal(err)
		}
		second, err := Acquire(context.Background(), "104.21.24.200")
		if err != nil {
			t.Fatal(err)
		}
		installed := h.saw("nft -f -")
		if nftFails {
			installed = h.saw("iptables -w 5 -t mangle -A PREROUTING")
		}
		if !installed || h.sysctl != "1" {
			t.Fatalf("nftFails=%v: restore installed %v, src_valid_mark %q: %v", nftFails, installed, h.sysctl, h.calls)
		}
		h.calls = nil
		first.Release()
		if len(h.calls) != 0 || h.sysctl != "1" {
			t.Fatalf("nftFails=%v: the first release tore down what the second pin needs: %v", nftFails, h.calls)
		}
		second.Release()
		removed := h.saw("nft delete table inet " + rpTable)
		if nftFails {
			removed = h.saw("iptables -w 5 -t mangle -D PREROUTING")
		}
		if !removed || h.sysctl != "0" {
			t.Fatalf("nftFails=%v: last release removed %v, src_valid_mark %q: %v", nftFails, removed, h.sysctl, h.calls)
		}
	}
}
