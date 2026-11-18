package sbrelay

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter/certificate"
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/adapter/outbound"
	boxservice "github.com/sagernet/sing-box/adapter/service"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/dns"
	dnslocal "github.com/sagernet/sing-box/dns/transport/local"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/direct"
	"github.com/sagernet/sing-box/protocol/vless"
	"github.com/sagernet/sing/common/json/badoption"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

const (
	testCDNHost = "cdn.pangea.test"
	testCDNUUID = "6f1c2a5e-3b7d-4e8a-9c0f-1d2e3f4a5b6c"
	testCDNPath = "/upgrade"
)

type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  string
}

func newTestCA(t *testing.T) testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Pangea Test Inspection CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return testCA{cert: cert, key: key, pem: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))}
}

func (ca testCA) leaf(t *testing.T, host string) (certPEM, keyPEM string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
}

// startCDNNode runs the node's cdn-in listener shape: VLESS over HTTPUpgrade behind TLS.
func startCDNNode(t *testing.T, certPEM, keyPEM string) int {
	t.Helper()
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()

	inbounds := inbound.NewRegistry()
	vless.RegisterInbound(inbounds)
	outbounds := outbound.NewRegistry()
	direct.RegisterOutbound(outbounds)
	transports := dns.NewTransportRegistry()
	dnslocal.RegisterTransport(transports)
	ctx, cancel := context.WithCancel(context.Background())
	ctx = box.Context(ctx, inbounds, outbounds, endpoint.NewRegistry(), transports, boxservice.NewRegistry(), certificate.NewRegistry())
	listen := badoption.Addr(netip.MustParseAddr("127.0.0.1"))
	server, err := box.New(box.Options{Context: ctx, Options: option.Options{
		Log: &option.LogOptions{Level: "warn"},
		Inbounds: []option.Inbound{{Type: C.TypeVLESS, Tag: "cdn-in", Options: &option.VLESSInboundOptions{
			ListenOptions: option.ListenOptions{Listen: &listen, ListenPort: uint16(port)},
			Users:         []option.VLESSUser{{UUID: testCDNUUID}},
			InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{TLS: &option.InboundTLSOptions{
				Enabled:     true,
				Certificate: badoption.Listable[string]{certPEM},
				Key:         badoption.Listable[string]{keyPEM},
			}},
			Transport: &option.V2RayTransportOptions{
				Type:               C.V2RayTransportTypeHTTPUpgrade,
				HTTPUpgradeOptions: option.V2RayHTTPUpgradeOptions{Path: testCDNPath},
			},
		}}},
		Outbounds: []option.Outbound{{Type: C.TypeDirect, Tag: "direct"}},
	}})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		server.Close()
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		server.Close()
		cancel()
	})
	return port
}

func testCDNProfile(port int, extraRoots string) state.CDNProfile {
	return state.CDNProfile{
		RemoteHost:    "127.0.0.1",
		RemotePort:    port,
		UUID:          testCDNUUID,
		ServerName:    testCDNHost,
		Path:          testCDNPath,
		TargetPort:    51820,
		ExtraRootsPEM: extraRoots,
	}
}

// A TLS-inspecting filter whose CA only the user trusts: refused by the system
// store alone, accepted once the app hands the daemon that root.
func TestCDNTrustsRootsTheUserTrusts(t *testing.T) {
	ca := newTestCA(t)
	certPEM, keyPEM := ca.leaf(t, testCDNHost)
	port := startCDNNode(t, certPEM, keyPEM)
	ctx := context.Background()

	refused := NewCDN(state.NewLogStore(50))
	err := refused.Start(ctx, testCDNProfile(port, ""))
	if err == nil {
		refused.Stop(ctx)
		t.Fatal("dial with only the system store succeeded against an untrusted CA")
	}
	if !strings.Contains(err.Error(), "inspecting TLS") {
		t.Fatalf("error does not name the untrusted certificate: %v", err)
	}

	trusted := NewCDN(state.NewLogStore(50))
	if err := trusted.Start(ctx, testCDNProfile(port, ca.pem)); err != nil {
		t.Fatalf("dial with the user's root failed: %v", err)
	}
	if err := trusted.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestCDNStillRefusesOtherCertificates(t *testing.T) {
	ca := newTestCA(t)
	certPEM, keyPEM := ca.leaf(t, testCDNHost)
	port := startCDNNode(t, certPEM, keyPEM)
	ctx := context.Background()

	m := NewCDN(state.NewLogStore(50))
	if err := m.Start(ctx, testCDNProfile(port, newTestCA(t).pem)); err == nil {
		m.Stop(ctx)
		t.Fatal("a root the user trusts vouched for a certificate it did not sign")
	}
}

func TestParseRootsSkipsWhatGoCannotUse(t *testing.T) {
	ca := newTestCA(t)
	bundle := strings.Join([]string{
		"-----BEGIN CERTIFICATE-----\nbm90IGEgY2VydGlmaWNhdGU=\n-----END CERTIFICATE-----\n",
		"-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n",
		ca.pem,
	}, "")
	roots := parseRoots(bundle)
	if len(roots) != 1 || roots[0] != ca.pem {
		t.Fatalf("parseRoots kept %d entries, want only the valid CA", len(roots))
	}
	if parseRoots("") != nil {
		t.Fatal("an empty bundle produced roots")
	}

	sp, err := buildCDN(testCDNProfile(443, bundle))
	if err != nil {
		t.Fatal(err)
	}
	if len(sp.extraRoots) != 1 || !strings.Contains(sp.remote, "trusting 1 of the user's own roots") {
		t.Fatalf("spec roots=%d remote=%q", len(sp.extraRoots), sp.remote)
	}
}
