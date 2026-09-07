package hysteria2

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/sagernet/sing-box/option"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

func validProfile() state.Hysteria2Profile {
	return state.Hysteria2Profile{
		LocalPort:    0,
		RemoteHost:   "hy2.example.com",
		RemotePort:   443,
		ServerName:   "hy2.example.com",
		Password:     "auth-pw",
		ObfsPassword: "obfs-pw",
		UpMbps:       100,
		DownMbps:     200,
	}
}

func TestValidateProfile(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(p state.Hysteria2Profile) state.Hysteria2Profile
		wantErr bool
	}{
		{"valid", func(p state.Hysteria2Profile) state.Hysteria2Profile { return p }, false},
		{"missing remoteHost", func(p state.Hysteria2Profile) state.Hysteria2Profile { p.RemoteHost = ""; return p }, true},
		{"zero remotePort", func(p state.Hysteria2Profile) state.Hysteria2Profile { p.RemotePort = 0; return p }, true},
		{"missing password", func(p state.Hysteria2Profile) state.Hysteria2Profile { p.Password = ""; return p }, true},
		{"missing obfsPassword", func(p state.Hysteria2Profile) state.Hysteria2Profile { p.ObfsPassword = ""; return p }, true},
		{"negative localPort", func(p state.Hysteria2Profile) state.Hysteria2Profile { p.LocalPort = -1; return p }, true},
		{"valid remotePorts", func(p state.Hysteria2Profile) state.Hysteria2Profile {
			p.RemotePorts = []string{"20000:29999", "40000:40000"}
			return p
		}, false},
		{"remotePorts no colon", func(p state.Hysteria2Profile) state.Hysteria2Profile {
			p.RemotePorts = []string{"20000-29999"}
			return p
		}, true},
		{"remotePorts reversed", func(p state.Hysteria2Profile) state.Hysteria2Profile {
			p.RemotePorts = []string{"29999:20000"}
			return p
		}, true},
		{"remotePorts out of range", func(p state.Hysteria2Profile) state.Hysteria2Profile {
			p.RemotePorts = []string{"20000:70000"}
			return p
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateProfile(tc.mutate(validProfile()))
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestBuildClientOptionsFieldMapping(t *testing.T) {
	profile := validProfile()
	profile.Insecure = true
	pin := []byte{1, 2, 3, 4}
	profile.PinSHA256 = base64.StdEncoding.EncodeToString(pin)

	opts, err := buildClientOptions(profile, 12345)
	if err != nil {
		t.Fatalf("buildClientOptions: %v", err)
	}

	if len(opts.Inbounds) != 1 || len(opts.Outbounds) != 1 {
		t.Fatalf("expected exactly one inbound and one outbound, got %d/%d", len(opts.Inbounds), len(opts.Outbounds))
	}

	mixedOpts, ok := opts.Inbounds[0].Options.(*option.HTTPMixedInboundOptions)
	if !ok {
		t.Fatalf("inbound options type = %T, want *option.HTTPMixedInboundOptions", opts.Inbounds[0].Options)
	}
	if mixedOpts.ListenPort != 12345 {
		t.Fatalf("mixed listen port = %d, want 12345", mixedOpts.ListenPort)
	}

	hyOpts, ok := opts.Outbounds[0].Options.(*option.Hysteria2OutboundOptions)
	if !ok {
		t.Fatalf("outbound options type = %T, want *option.Hysteria2OutboundOptions", opts.Outbounds[0].Options)
	}
	if hyOpts.Server != profile.RemoteHost || hyOpts.ServerPort != uint16(profile.RemotePort) {
		t.Fatalf("server = %s:%d, want %s:%d", hyOpts.Server, hyOpts.ServerPort, profile.RemoteHost, profile.RemotePort)
	}
	if hyOpts.Password != profile.Password {
		t.Fatalf("password = %q, want %q", hyOpts.Password, profile.Password)
	}
	if hyOpts.Obfs == nil || hyOpts.Obfs.Type != obfsTypeSalamander || hyOpts.Obfs.Password != profile.ObfsPassword {
		t.Fatalf("obfs = %+v, want type=%s password=%q", hyOpts.Obfs, obfsTypeSalamander, profile.ObfsPassword)
	}
	if hyOpts.UpMbps != profile.UpMbps || hyOpts.DownMbps != profile.DownMbps {
		t.Fatalf("mbps = %d/%d, want %d/%d", hyOpts.UpMbps, hyOpts.DownMbps, profile.UpMbps, profile.DownMbps)
	}
	if hyOpts.TLS == nil || !hyOpts.TLS.Enabled || !hyOpts.TLS.Insecure || hyOpts.TLS.ServerName != profile.ServerName {
		t.Fatalf("tls = %+v, want enabled+insecure+serverName=%q", hyOpts.TLS, profile.ServerName)
	}
	if len(hyOpts.TLS.CertificatePublicKeySHA256) != 1 || string(hyOpts.TLS.CertificatePublicKeySHA256[0]) != string(pin) {
		t.Fatalf("cert pin = %v, want %v", hyOpts.TLS.CertificatePublicKeySHA256, pin)
	}
}

func TestBuildClientOptionsPortHopping(t *testing.T) {
	profile := validProfile()
	profile.RemotePorts = []string{"20000:29999", "40000:40000"}
	opts, err := buildClientOptions(profile, 1)
	if err != nil {
		t.Fatalf("buildClientOptions: %v", err)
	}
	hyOpts := opts.Outbounds[0].Options.(*option.Hysteria2OutboundOptions)
	if len(hyOpts.ServerPorts) != 2 || hyOpts.ServerPorts[0] != "20000:29999" || hyOpts.ServerPorts[1] != "40000:40000" {
		t.Fatalf("ServerPorts = %v, want the two ranges", hyOpts.ServerPorts)
	}
	if time.Duration(hyOpts.HopInterval) != hysteria2HopInterval {
		t.Fatalf("HopInterval = %s, want %s", time.Duration(hyOpts.HopInterval), hysteria2HopInterval)
	}

	// No RemotePorts means no hopping: ServerPorts empty, HopInterval unset.
	noHop, err := buildClientOptions(validProfile(), 1)
	if err != nil {
		t.Fatalf("buildClientOptions: %v", err)
	}
	base := noHop.Outbounds[0].Options.(*option.Hysteria2OutboundOptions)
	if len(base.ServerPorts) != 0 || base.HopInterval != 0 {
		t.Fatalf("without RemotePorts: ServerPorts=%v HopInterval=%v, want empty/0", base.ServerPorts, base.HopInterval)
	}
}

func TestBuildClientOptionsRejectsInvalidPin(t *testing.T) {
	profile := validProfile()
	profile.PinSHA256 = "not-valid-base64!!"
	if _, err := buildClientOptions(profile, 1); err == nil {
		t.Fatalf("expected error for invalid pinSha256, got nil")
	}
}
