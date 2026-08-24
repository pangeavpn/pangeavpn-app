package platform

// SystemEvent is a host-level signal the recovery logic should react to
// immediately instead of waiting for its next timer.
type SystemEvent int

const (
	// SystemEventResumed fires when the host wakes from sleep or hibernation.
	SystemEventResumed SystemEvent = iota
	// SystemEventNetworkChanged fires when the host's network connectivity
	// changes in any direction; the consumer re-evaluates, it does not trust it.
	SystemEventNetworkChanged
)
