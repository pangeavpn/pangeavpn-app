package state

import (
	"slices"
	"sync"
)

type Machine struct {
	mu     sync.RWMutex
	state  DaemonState
	detail string
}

func NewMachine() *Machine {
	return &Machine{state: StateDisconnected, detail: "idle"}
}

func (m *Machine) Set(state DaemonState, detail string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = state
	m.detail = detail
}

// CompareAndSet applies the transition only if the current state is one of
// expected, atomically with the check. Reports whether it was applied.
func (m *Machine) CompareAndSet(expected []DaemonState, state DaemonState, detail string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !slices.Contains(expected, m.state) {
		return false
	}
	m.state = state
	m.detail = detail
	return true
}

func (m *Machine) Get() (DaemonState, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state, m.detail
}
