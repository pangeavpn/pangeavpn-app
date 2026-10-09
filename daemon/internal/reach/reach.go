// Package reach asks the hub whether the host's own network still reaches the
// internet, over a connection that stays outside the VPN tunnel.
package reach

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"time"
)

// Outcome is what one probe learned about the path it took.
type Outcome int

const (
	// Silent: no HTTP reply came back, so nothing on the path answered.
	Silent Outcome = iota
	// Refused: something answered over HTTP, but not with our nonce.
	Refused
	// Answered: the hub echoed our nonce, so this path reaches the internet.
	Answered
)

func (o Outcome) String() string {
	switch o {
	case Answered:
		return "answered"
	case Refused:
		return "refused"
	default:
		return "silent"
	}
}

const (
	// HubHost goes in the Host header only; the TLS handshake carries no SNI.
	HubHost = "api.pangeavpn.org"
	HubPort = 443
	Path    = "/api/client/reach"
	// Timeout bounds one probe end to end, dial included.
	Timeout = 4 * time.Second

	nonceBytes   = 16
	maxBodyBytes = 1024
	// maxReplyBytes caps the whole reply: with no certificate check, an on-path
	// box could otherwise stream headers into the daemon until the deadline.
	maxReplyBytes = 16 << 10
)

// DialFunc opens a raw stream to the hub's TLS port.
type DialFunc func(ctx context.Context) (net.Conn, error)

// Probe asks the hub, over dial, to echo a fresh nonce. The certificate is not
// checked: nothing secret is sent, and only the echo proves the hub answered.
func Probe(ctx context.Context, dial DialFunc) Outcome {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	nonce, err := newNonce()
	if err != nil {
		return Silent
	}
	conn, err := dial(ctx)
	if err != nil {
		return Silent
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	// No ServerName means no SNI, like the app's direct-IP path: a filter that
	// blocks the hub by name never sees it.
	tlsConn := tls.Client(conn, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		return Silent
	}
	return exchange(tlsConn, nonce)
}

func exchange(conn net.Conn, nonce string) Outcome {
	req, err := http.NewRequest(http.MethodGet, "https://"+HubHost+Path+"?n="+nonce, nil)
	if err != nil {
		return Silent
	}
	req.Header.Set("User-Agent", "")
	req.Close = true
	if err := req.Write(conn); err != nil {
		return Silent
	}
	resp, err := http.ReadResponse(bufio.NewReader(io.LimitReader(conn, maxReplyBytes)), req)
	if err != nil {
		return Silent
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Refused
	}
	var body struct {
		N string `json:"n"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(&body); err != nil || body.N != nonce {
		return Refused
	}
	return Answered
}

func newNonce() (string, error) {
	buf := make([]byte, nonceBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
