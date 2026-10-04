package egress

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"syscall"
	"time"
)

// Broker frames are a 4-byte big-endian length and a JSON body; a successful
// response carries exactly one descriptor, sent with the frame's first byte.
const (
	brokerOpTCP          = "tcp"
	brokerOpUDP          = "udp"
	brokerMaxFrame       = 16 << 10
	brokerDefaultTimeout = 20 * time.Second
	brokerMaxTimeout     = 60 * time.Second
)

var (
	errBrokerFrame    = errors.New("split egress broker: bad frame")
	errBrokerProtocol = errors.New("split egress broker: protocol error")
)

type brokerRequest struct {
	ID        uint64 `json:"id"`
	Op        string `json:"op"`
	IfIndex   int    `json:"ifindex"`
	Addr      string `json:"addr,omitempty"`
	TimeoutMs int64  `json:"timeoutMs,omitempty"`
}

type brokerResponse struct {
	ID    uint64 `json:"id"`
	Err   string `json:"err,omitempty"`
	Errno int    `json:"errno,omitempty"`
}

// brokerError keeps the broker-side errno so IsUnreachable works across the process boundary.
type brokerError struct {
	msg   string
	errno syscall.Errno
}

func (e *brokerError) Error() string { return "split egress broker: " + e.msg }

func (e *brokerError) Unwrap() error {
	if e.errno == 0 {
		return nil
	}
	return e.errno
}

func (r brokerResponse) error() error {
	if r.Err == "" {
		return nil
	}
	return &brokerError{msg: r.Err, errno: syscall.Errno(r.Errno)}
}

func responseFor(id uint64, err error) brokerResponse {
	resp := brokerResponse{ID: id}
	if err != nil {
		resp.Err = err.Error()
		var errno syscall.Errno
		if errors.As(err, &errno) {
			resp.Errno = int(errno)
		}
	}
	return resp
}

func (r brokerRequest) timeout() time.Duration {
	if r.TimeoutMs <= 0 {
		return brokerDefaultTimeout
	}
	return min(time.Duration(r.TimeoutMs)*time.Millisecond, brokerMaxTimeout)
}

// validate returns the TCP destination; UDP sockets always bind 0.0.0.0:0.
func (r brokerRequest) validate() (netip.AddrPort, error) {
	if r.IfIndex <= 0 {
		return netip.AddrPort{}, ErrNoInterface
	}
	switch r.Op {
	case brokerOpUDP:
		return netip.AddrPort{}, nil
	case brokerOpTCP:
		ap, err := netip.ParseAddrPort(r.Addr)
		if err != nil || !ap.Addr().Is4() || ap.Addr().IsUnspecified() || ap.Port() == 0 {
			return netip.AddrPort{}, fmt.Errorf("%w: bad address", errBrokerProtocol)
		}
		return ap, nil
	}
	return netip.AddrPort{}, fmt.Errorf("%w: unknown op", errBrokerProtocol)
}

func encodeFrame(v any) ([]byte, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if len(body) > brokerMaxFrame {
		return nil, errBrokerFrame
	}
	frame := make([]byte, 4, 4+len(body))
	binary.BigEndian.PutUint32(frame, uint32(len(body)))
	return append(frame, body...), nil
}

// splitFrame returns the first complete frame body in buf and the bytes after it.
func splitFrame(buf []byte) (body, rest []byte, ok bool, err error) {
	if len(buf) < 4 {
		return nil, buf, false, nil
	}
	n := binary.BigEndian.Uint32(buf)
	if n == 0 || n > brokerMaxFrame {
		return nil, buf, false, errBrokerFrame
	}
	if len(buf) < 4+int(n) {
		return nil, buf, false, nil
	}
	return buf[4 : 4+n], buf[4+n:], true, nil
}

func decodeRequest(body []byte) (brokerRequest, error) {
	var req brokerRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return brokerRequest{}, fmt.Errorf("%w: %v", errBrokerProtocol, err)
	}
	return req, nil
}

func decodeResponse(body []byte) (brokerResponse, error) {
	var resp brokerResponse
	if err := json.Unmarshal(body, &resp); err != nil || resp.ID == 0 {
		return brokerResponse{}, errBrokerProtocol
	}
	return resp, nil
}
