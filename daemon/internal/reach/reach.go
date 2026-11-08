// Package reach asks the hub whether the host's own network still reaches the
// internet, over a connection that stays outside the VPN tunnel. The question
// travels sealed on /v2/secure like every other hub request.
package reach

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/securechannel"
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
	maxBodyBytes = 4096
	// maxReplyBytes caps the whole reply: with no certificate check, an on-path
	// box could otherwise stream headers into the daemon until the deadline.
	maxReplyBytes = 16 << 10
)

// DialFunc opens a raw stream to the hub's TLS port.
type DialFunc func(ctx context.Context) (net.Conn, error)

// hubKeys are the keys every probe is sealed for; tests swap in a stand-in hub's.
var hubKeys = securechannel.HubKeys

// Probe asks the hub, over dial, to echo a fresh nonce. The certificate is not
// checked: the request is sealed for the hub's pinned keys, and only a reply
// that opens under this request's key, carrying the nonce, proves the hub answered.
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
	sealed, err := securechannel.Seal(hubKeys(), http.MethodGet, Path+"?n="+nonce, nil, nil)
	if err != nil {
		// Nothing was sent, so the path is unjudged; silence would count against it.
		return Refused
	}
	req, err := http.NewRequest(http.MethodPost, "https://"+HubHost+securechannel.Route, bytes.NewReader(sealed.Envelope))
	if err != nil {
		return Refused
	}
	req.Header.Set("Content-Type", "application/json")
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
	reply, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return Refused
	}
	// A middlebox can answer 200, but only the hub can seal a reply for this request.
	inner, err := sealed.Open(reply)
	if err != nil || inner.Status != http.StatusOK {
		return Refused
	}
	var body struct {
		N string `json:"n"`
	}
	if err := json.Unmarshal(inner.Body, &body); err != nil || body.N != nonce {
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
