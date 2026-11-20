package sbrelay

import (
	"strings"
	"testing"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
)

// cdn and cdnws dial the same edge with the same profile shape; only the protocol differs.
func TestCDNTransportsDifferOnlyInProtocol(t *testing.T) {
	profile := testCDNProfile(443, "")

	vless, err := buildCDN("cdn", cdnVLESS, profile)
	if err != nil {
		t.Fatal(err)
	}
	opts, ok := vless.outbound.Options.(*option.VLESSOutboundOptions)
	if vless.outbound.Type != C.TypeVLESS || !ok || opts.Transport.Type != C.V2RayTransportTypeHTTPUpgrade || opts.Transport.HTTPUpgradeOptions.Path != testCDNPath {
		t.Fatalf("cdn outbound = %s %+v, want VLESS over HTTPUpgrade on %s", vless.outbound.Type, vless.outbound.Options, testCDNPath)
	}

	vmess, err := buildCDN("cdnws", cdnVMess, profile)
	if err != nil {
		t.Fatal(err)
	}
	wsOpts, ok := vmess.outbound.Options.(*option.VMessOutboundOptions)
	if vmess.outbound.Type != C.TypeVMess || !ok || wsOpts.Transport.Type != C.V2RayTransportTypeWebsocket || wsOpts.Transport.WebsocketOptions.Path != testCDNPath {
		t.Fatalf("cdnws outbound = %s %+v, want VMess over a WebSocket on %s", vmess.outbound.Type, vmess.outbound.Options, testCDNPath)
	}
	if got := wsOpts.Transport.WebsocketOptions.Headers["Host"]; len(got) != 1 || got[0] != testCDNHost {
		t.Fatalf("cdnws Host header = %v, want %s", got, testCDNHost)
	}
	if !strings.HasSuffix(vless.remote, "VLESS over HTTPUpgrade") || !strings.HasSuffix(vmess.remote, "VMess over WebSocket") {
		t.Fatalf("remote = %q / %q, want each to name its protocol", vless.remote, vmess.remote)
	}
}

func TestCDNWSErrorsNameTheTransport(t *testing.T) {
	profile := testCDNProfile(443, "")
	profile.UUID = ""
	if _, err := NewCDNWS(nil).build(profile); err == nil || !strings.HasPrefix(err.Error(), "cdnws ") {
		t.Fatalf("err = %v, want one naming cdnws", err)
	}
}
