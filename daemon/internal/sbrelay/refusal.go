package sbrelay

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

const (
	probeTag     = "relay-probe"
	probeTimeout = 5 * time.Second
)

// refusalProbe asks the edge again, over the relay's own route, who refused the upgrade:
// the refused handshake itself keeps no headers or body.
type refusalProbe struct {
	tag        string
	at         *option.ServerOptions
	serverName string
	path       string
	roots      []string
	ech        []byte
	// websocket sends a real WebSocket key, as the VMess route does; HTTPUpgrade servers 404 one.
	websocket bool
}

func (r *refusalProbe) annotate(ctx context.Context, engine *box.Box, err error) error {
	msg := err.Error()
	// Go and Windows say "unknown authority"; the macOS verifier says "is not trusted".
	if strings.Contains(msg, "unknown authority") || strings.Contains(msg, "is not trusted") {
		return fmt.Errorf("%w (neither this device nor its user trusts the certificate; a network filter may be inspecting TLS)", err)
	}
	if !refusedUpgrade(err) {
		return err
	}
	if who := r.whoRefused(ctx, engine); who != "" {
		return fmt.Errorf("%w (%s)", err, who)
	}
	return err
}

// refusedUpgrade reports an HTTP answer other than the upgrade: the TLS session reached a server.
func refusedUpgrade(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "unexpected status") || strings.Contains(msg, "unexpected HTTP response status")
}

func (r *refusalProbe) whoRefused(ctx context.Context, engine *box.Box) string {
	out, ok := engine.Outbound().Outbound(r.tag)
	if !ok {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	roots, err := x509.SystemCertPool()
	if err != nil {
		roots = x509.NewCertPool()
	}
	for _, root := range r.roots {
		roots.AppendCertsFromPEM([]byte(root))
	}
	tlsConn, err := r.handshake(ctx, out, roots, r.ech, true)
	if err != nil {
		return ""
	}
	defer tlsConn.Close()
	upgrade := "Connection: Upgrade\r\nUpgrade: websocket\r\n"
	if r.websocket {
		var key [16]byte
		rand.Read(key[:])
		upgrade += "Sec-WebSocket-Version: 13\r\nSec-WebSocket-Key: " + base64.StdEncoding.EncodeToString(key[:]) + "\r\n"
	}
	path := r.path
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	request := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUser-Agent: %s\r\n%s\r\n", path, r.serverName, browserUserAgent, upgrade)
	if _, err := io.WriteString(tlsConn, request); err != nil {
		return ""
	}
	resp, err := http.ReadResponse(bufio.NewReader(tlsConn), nil)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusSwitchingProtocols {
		return "a second attempt was let through"
	}

	who := "a server that sends no Server header"
	if server := resp.Header.Get("Server"); server != "" {
		who = fmt.Sprintf("server %q", server)
	}
	if mitigated := resp.Header.Get("Cf-Mitigated"); mitigated != "" {
		who += fmt.Sprintf(" (Cloudflare %q)", mitigated)
	}
	refused := fmt.Sprintf("refused by %s: %s", who, resp.Status)
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if title := pageTitle(body); title != "" {
		refused += fmt.Sprintf(", page titled %q", title)
	}
	return refused + "; " + r.issuedBy(tlsConn.ConnectionState())
}

func (r *refusalProbe) handshake(ctx context.Context, out adapter.Outbound, roots *x509.CertPool, ech []byte, retry bool) (*tls.Conn, error) {
	conn, err := out.DialContext(ctx, N.NetworkTCP, M.ParseSocksaddrHostPort(r.at.Server, r.at.ServerPort))
	if err != nil {
		return nil, err
	}
	deadline, _ := ctx.Deadline()
	conn.SetDeadline(deadline)
	tlsConn := tls.Client(conn, &tls.Config{ServerName: r.serverName, RootCAs: roots, NextProtos: []string{"http/1.1"}, EncryptedClientHelloConfigList: ech})
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		conn.Close()
		var rejected *tls.ECHRejectionError
		if retry && errors.As(err, &rejected) && len(rejected.RetryConfigList) > 0 {
			return r.handshake(ctx, out, roots, rejected.RetryConfigList, false)
		}
		return nil, err
	}
	return tlsConn, nil
}

// issuedBy names the certificate's issuer. A chain only the user's own roots complete
// means a TLS-inspecting filter answered in the edge's place.
func (r *refusalProbe) issuedBy(state tls.ConnectionState) string {
	if len(state.PeerCertificates) == 0 {
		return "no certificate"
	}
	leaf := state.PeerCertificates[0]
	issuer := leaf.Issuer.CommonName
	if issuer == "" && len(leaf.Issuer.Organization) > 0 {
		issuer = leaf.Issuer.Organization[0]
	}
	intermediates := x509.NewCertPool()
	for _, cert := range state.PeerCertificates[1:] {
		intermediates.AddCert(cert)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{DNSName: r.serverName, Intermediates: intermediates}); err != nil && len(r.roots) > 0 {
		return fmt.Sprintf("certificate from %q, a root only this user trusts, so a TLS-inspecting network filter answered instead of the edge", issuer)
	}
	return fmt.Sprintf("certificate from %q", issuer)
}

var titlePattern = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

// pageTitle keeps only an HTML title, which names a block page without copying what it says
// about the user.
func pageTitle(body []byte) string {
	match := titlePattern.FindSubmatch(body)
	if match == nil {
		return ""
	}
	title := []rune(strings.Join(strings.Fields(string(match[1])), " "))
	if len(title) > 80 {
		title = title[:80]
	}
	return string(title)
}
