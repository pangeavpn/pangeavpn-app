//go:build !windows && !linux && !darwin

package egress

func New(opts Options) (Dialer, error) { return nil, ErrUnsupported }

func RunBroker() int { return 2 }
