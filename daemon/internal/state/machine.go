package state

import "sync"

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

func (m *Machine) Get() (DaemonState, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state, m.detail
}
