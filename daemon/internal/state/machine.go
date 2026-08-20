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

// CompareAndSet applies the transition only if the current state is one of
// expected, atomically with the check. Callers that would otherwise read
// Get() then Set() outside their own lock (e.g. a background recovery
// goroutine racing a Disconnect) should use this instead, so a state change
// that happened in between isn't silently overwritten. Reports whether the
// transition was applied.
func (m *Machine) CompareAndSet(expected []DaemonState, state DaemonState, detail string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !containsState(expected, m.state) {
		return false
	}
	m.state = state
	m.detail = detail
	return true
}

func containsState(states []DaemonState, target DaemonState) bool {
	for _, s := range states {
		if s == target {
			return true
		}
	}
	return false
}

func (m *Machine) Get() (DaemonState, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state, m.detail
}
