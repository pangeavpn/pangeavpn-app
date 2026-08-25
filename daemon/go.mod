module github.com/pangeavpn/pangeavpn-desktop/daemon

go 1.26.0

// go1.26.0/1.26.1 Green Tea GC corrupts pointers on Windows (golang/go#77975).
toolchain go1.26.7

require (
	github.com/clarkduvall/hyperloglog v0.0.0-20171127014514-a0107a5d8004
	github.com/godbus/dbus/v5 v5.2.2
	github.com/sagernet/sing v0.8.14
	github.com/sagernet/sing-box v1.13.19
	github.com/sirupsen/logrus v1.10.1
	github.com/vishvananda/netlink v1.3.1
	gitlab.torproject.org/tpo/anti-censorship/geoip v0.0.0-20210928150955-7ce4b3d98d01
	gitlab.torproject.org/tpo/anti-censorship/pluggable-transports/snowflake/v2 v2.14.1
	golang.org/x/crypto v0.55.0
	golang.org/x/sys v0.47.0
	golang.zx2c4.com/wireguard v0.0.0-20260522210424-ecfc5a8d5446
	golang.zx2c4.com/wireguard/windows v1.0.1
)

require (
	github.com/aws/aws-sdk-go-v2 v1.43.7 // indirect
	github.com/aws/aws-sdk-go-v2/config v1.32.38 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.19.37 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.18.38 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.38 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.38 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.4.39 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.17 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.38 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.5.7 // indirect
	github.com/aws/aws-sdk-go-v2/service/sqs v1.46.7 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.33.7 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.38.7 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.45.7 // indirect
	github.com/aws/smithy-go v1.27.9 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/caddyserver/certmagic v0.25.4 // indirect
	github.com/caddyserver/zerossl v0.1.5 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/database64128/netx-go v0.1.1 // indirect
	github.com/database64128/tfo-go/v2 v2.3.3 // indirect
	github.com/florianl/go-nfqueue/v2 v2.1.0 // indirect
	github.com/fsnotify/fsnotify v1.10.1 // indirect
	github.com/go-chi/chi/v5 v5.3.2 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/gobwas/httphead v0.1.0 // indirect
	github.com/gobwas/pool v0.2.1 // indirect
	github.com/gofrs/uuid/v5 v5.5.1 // indirect
	github.com/golang/mock v1.6.0 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/hashicorp/yamux v0.1.2 // indirect
	github.com/insomniacslk/dhcp v0.0.0-20260728151720-c308df0fdcef // indirect
	github.com/klauspost/cpuid/v2 v2.4.0 // indirect
	github.com/klauspost/reedsolomon v1.14.2 // indirect
	github.com/libdns/acmedns v0.5.0 // indirect
	github.com/libdns/alidns v1.0.7 // indirect
	github.com/libdns/cloudflare v0.2.2 // indirect
	github.com/libdns/libdns v1.1.1 // indirect
	github.com/logrusorgru/aurora v2.0.3+incompatible // indirect
	github.com/mdlayher/netlink v1.11.2 // indirect
	github.com/mdlayher/socket v0.6.1 // indirect
	github.com/metacubex/utls v1.8.7 // indirect
	github.com/mholt/acmez/v3 v3.1.6 // indirect
	github.com/miekg/dns v1.1.73 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/patrickmn/go-cache v2.1.0+incompatible // indirect
	github.com/pierrec/lz4/v4 v4.1.29 // indirect
	github.com/pion/datachannel v1.6.2 // indirect
	github.com/pion/dtls/v3 v3.1.5 // indirect
	github.com/pion/ice/v4 v4.4.1 // indirect
	github.com/pion/interceptor v0.1.47 // indirect
	github.com/pion/logging v0.2.4 // indirect
	github.com/pion/mdns/v2 v2.1.0 // indirect
	github.com/pion/randutil v0.1.0 // indirect
	github.com/pion/rtcp v1.2.17 // indirect
	github.com/pion/rtp v1.10.5 // indirect
	github.com/pion/sctp v1.11.1 // indirect
	github.com/pion/sdp/v3 v3.0.19 // indirect
	github.com/pion/srtp/v3 v3.0.13 // indirect
	github.com/pion/stun/v3 v3.1.7 // indirect
	github.com/pion/transport/v4 v4.1.0 // indirect
	github.com/pion/turn/v5 v5.0.13 // indirect
	github.com/pion/webrtc/v4 v4.2.18 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/prometheus/client_golang v1.23.2 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.66.1 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	github.com/quic-go/qpack v0.6.0 // indirect
	github.com/realclientip/realclientip-go v1.0.0 // indirect
	github.com/sagernet/bbolt v0.0.0-20231014093535-ea5cb2fe9f0a // indirect
	github.com/sagernet/fswatch v0.1.2 // indirect
	github.com/sagernet/gvisor v0.0.0-20250811.0-sing-box-mod.1 // indirect
	github.com/sagernet/netlink v0.0.0-20260814022025-64455d367bbf // indirect
	github.com/sagernet/nftables v0.3.0-mod.4 // indirect
	github.com/sagernet/quic-go v0.59.0-sing-box-mod.4 // indirect
	github.com/sagernet/sing-mux v0.3.5 // indirect
	github.com/sagernet/sing-quic v0.6.4-0.20260803041914-d83826c306d7 // indirect
	github.com/sagernet/sing-shadowsocks v0.2.9 // indirect
	github.com/sagernet/sing-shadowsocks2 v0.2.2 // indirect
	github.com/sagernet/sing-tun v0.8.12-0.20260810140523-7c73233bd0fb // indirect
	github.com/sagernet/sing-vmess v0.2.8-0.20250909125414-3aed155119a1 // indirect
	github.com/sagernet/smux v1.5.50-sing-box-mod.1 // indirect
	github.com/sagernet/ws v0.0.0-20231204124109-acfe8907c854 // indirect
	github.com/theodorsm/covert-dtls v1.5.1 // indirect
	github.com/tjfoc/gmsm v1.4.1 // indirect
	github.com/txthinking/runnergroup v0.0.0-20250224021307-5864ffeb65ae // indirect
	github.com/txthinking/socks5 v0.0.0-20260601051520-339b044ab0eb // indirect
	github.com/u-root/uio v0.0.0-20240224005618-d2acac8f3701 // indirect
	github.com/wlynxg/anet v0.0.5 // indirect
	github.com/xtaci/kcp-go/v5 v5.6.72 // indirect
	github.com/xtaci/smux v1.5.57 // indirect
	github.com/zeebo/blake3 v0.2.4 // indirect
	gitlab.torproject.org/tpo/anti-censorship/pluggable-transports/ptutil v0.0.0-20250815012447-418f76dcf315 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.28.0 // indirect
	go.uber.org/zap/exp v0.3.0 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	go4.org/netipx v0.0.0-20260823151212-3075585bcbeb // indirect
	golang.org/x/exp v0.0.0-20260824195058-e88cd73687aa // indirect
	golang.org/x/mod v0.40.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260819154853-08b0e4226688 // indirect
	google.golang.org/grpc v1.83.1 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
	lukechampine.com/blake3 v1.4.1 // indirect
)

require (
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/juju/ratelimit v1.0.2 // indirect
	github.com/pangeavpn/cloak v0.0.0-20260511185428-bf0aeb6365cb
	github.com/refraction-networking/utls v1.8.2 // indirect
)

require (
	github.com/andybalholm/brotli v1.2.2 // indirect
	github.com/klauspost/compress v1.19.2 // indirect
	github.com/vishvananda/netns v0.0.5 // indirect
	golang.org/x/net v0.58.0
	golang.org/x/text v0.41.0
	golang.zx2c4.com/wintun v0.0.0-20230126152724-0fa3db229ce2
)
