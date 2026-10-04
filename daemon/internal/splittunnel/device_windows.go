package splittunnel

// The daemon's Windows MTU resize and adapter lookups type-assert these on the session device.
var _ interface {
	ForceMTU(int)
	LUID() uint64
} = (*Device)(nil)
