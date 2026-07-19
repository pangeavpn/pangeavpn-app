//go:build !naive_cgo

package main

import (
	"context"
	"errors"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// naiveStub satisfies transport.Manager with a permanent "not available"
// failure, so profile.Naive fallback cleanly no-ops in builds without the
// naive_cgo tag (e.g. this dev machine's plain `go build`/`go test`).
type naiveStub struct{}

func newNaiveManager(logs *state.LogStore) *naiveStub {
	_ = logs
	return &naiveStub{}
}

func (naiveStub) Start(ctx context.Context, profile state.NaiveProfile) error {
	return errors.New("naive transport not available in this build")
}

func (naiveStub) Stop(ctx context.Context) error { return nil }

func (naiveStub) Status() state.TransportStatus { return state.TransportStatus{} }
