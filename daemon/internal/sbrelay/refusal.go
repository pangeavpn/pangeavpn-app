package sbrelay

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	box "github.com/sagernet/sing-box"
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
}

func (r *refusalProbe) annotate(ctx context.Context, engine *box.Box, err error) error {
	msg := err.Error()
	// Go and Windows say "unknown authority"; the macOS verifier says "is not trusted".
	if strings.Contains(msg, "unknown authority") || strings.Contains(msg, "is not trusted") {
		return fmt.Errorf("%w (neither this device nor its user trusts the certificate; a network filter may be inspecting TLS)", err)
	}
	if !strings.Contains(msg, "unexpected status") && !strings.Contains(msg, "unexpected HTTP response status") {
		return err
	}
	if who := r.whoRefused(ctx, engine); who != "" {
		return fmt.Errorf("%w (%s)", err, who)
	}
	return err
}

func (r *refusalProbe) whoRefused(ctx context.Context, engine *box.Box) string {
	out, ok := engine.Outbound().Outbound(r.tag)
	if !ok {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	conn, err := out.DialContext(ctx, N.NetworkTCP, M.ParseSocksaddrHostPort(r.at.Server, r.at.ServerPort))
	if err != nil {
		return ""
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	conn.SetDeadline(deadline)

	roots, err := x509.SystemCertPool()
	if err != nil {
		roots = x509.NewCertPool()
	}
	for _, root := range r.roots {
		roots.AppendCertsFromPEM([]byte(root))
	}
	tlsConn := tls.Client(conn, &tls.Config{ServerName: r.serverName, RootCAs: roots, NextProtos: []string{"http/1.1"}})
	var key [16]byte
	rand.Read(key[:])
	request := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUser-Agent: %s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: %s\r\n\r\n",
		r.path, r.serverName, browserUserAgent, base64.StdEncoding.EncodeToString(key[:]))
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
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if title := pageTitle(body); title != "" {
		return fmt.Sprintf("refused by %s: %s, page titled %q", who, resp.Status, title)
	}
	return fmt.Sprintf("refused by %s: %s", who, resp.Status)
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
