package transport

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestIsNetworkChanged(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"bare", errors.New("network changed"), true},
		{"wrapped", errors.New("hysteria2: session handshake failed: network changed"), true},
		{"other", errors.New("connection refused"), false},
		{"deadline", context.DeadlineExceeded, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsNetworkChanged(tc.err); got != tc.want {
				t.Fatalf("IsNetworkChanged(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestListenPacketResilientRetriesNetworkChange(t *testing.T) {
	var calls int
	want := &net.UDPConn{}
	pc, err := ListenPacketResilient(context.Background(), func(context.Context) (net.PacketConn, error) {
		calls++
		if calls < 3 {
			return nil, errors.New("network changed")
		}
		return want, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pc != net.PacketConn(want) {
		t.Fatalf("got conn %v, want %v", pc, want)
	}
	if calls != 3 {
		t.Fatalf("dialed %d times, want 3", calls)
	}
}

func TestListenPacketResilientReturnsOtherErrorsImmediately(t *testing.T) {
	var calls int
	sentinel := errors.New("connection refused")
	_, err := ListenPacketResilient(context.Background(), func(context.Context) (net.PacketConn, error) {
		calls++
		return nil, sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("got %v, want %v", err, sentinel)
	}
	if calls != 1 {
		t.Fatalf("dialed %d times, want 1 (no retry on a real failure)", calls)
	}
}

func TestListenPacketResilientStopsWhenBudgetSpent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	changed := errors.New("network changed")
	var calls int
	_, err := ListenPacketResilient(ctx, func(context.Context) (net.PacketConn, error) {
		calls++
		return nil, changed
	})
	// The last error is the network-change reset, not the bare context error, so
	// the log still names the cause.
	if !errors.Is(err, changed) {
		t.Fatalf("got %v, want the network-changed error", err)
	}
	if calls == 0 {
		t.Fatal("expected at least one dial")
	}
}

func TestListenPacketResilientSucceedsFirstTry(t *testing.T) {
	var calls int
	want := &net.UDPConn{}
	pc, err := ListenPacketResilient(context.Background(), func(context.Context) (net.PacketConn, error) {
		calls++
		return want, nil
	})
	if err != nil || pc != net.PacketConn(want) || calls != 1 {
		t.Fatalf("got pc=%v err=%v calls=%d, want one successful dial", pc, err, calls)
	}
}
