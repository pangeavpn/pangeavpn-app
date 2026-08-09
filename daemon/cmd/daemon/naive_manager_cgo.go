//go:build naive_cgo

package main

import (
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/naive"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

func newNaiveManager(logs *state.LogStore) *naive.Manager {
	return naive.NewManager(logs)
}
