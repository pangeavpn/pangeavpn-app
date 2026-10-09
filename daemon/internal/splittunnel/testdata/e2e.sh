#!/usr/bin/env bash

# Build from daemon/: CGO_ENABLED=0 GOOS=linux go test -c -o DIR/splittunnel.test ./internal/splittunnel; CGO_ENABLED=0 GOOS=linux go build -o DIR/app ./internal/splittunnel/testdata/e2eclient
# Run as root on Linux: e2e.sh DIR/splittunnel.test DIR/app [test flags] (or set ST_E2E_TEST/ST_E2E_APP); only its own network namespaces are touched.
set -u
TEST_BIN=${ST_E2E_TEST:-}
APP_BIN=${ST_E2E_APP:-}
if [ $# -ge 2 ] && [ "${1#-}" = "$1" ]; then
	TEST_BIN=$1 APP_BIN=$2
	shift 2
fi
if [ ! -f "$TEST_BIN" ] || [ ! -f "$APP_BIN" ]; then
	echo "usage: $0 <splittunnel.test> <e2eclient binary> [test flags]" >&2
	exit 2
fi
CNS=st-e2e-client
NNS=st-e2e-net
DIR=
LAUNCHER_PID=

cleanup() {
	if [ -n "$LAUNCHER_PID" ]; then
		kill "$LAUNCHER_PID" 2>/dev/null
		wait "$LAUNCHER_PID" 2>/dev/null
	fi
	for ns in $CNS $NNS; do
		if ip netns list | grep -qw "$ns"; then
			ip netns pids "$ns" 2>/dev/null | xargs -r kill -9 2>/dev/null
			ip netns del "$ns"
		fi
	done
	if [ -n "$DIR" ]; then
		rm -rf "$DIR"
	fi
}

cleanup
trap cleanup EXIT

set -e
DIR=$(mktemp -d "${TMPDIR:-/tmp}/st-e2e.XXXXXX")
ip netns add $NNS
ip netns add $CNS
ip -n $CNS link add st-e2e-c type veth peer name st-e2e-n netns $NNS
ip -n $NNS addr add 10.99.0.1/24 dev st-e2e-n
ip -n $NNS link set lo up
ip -n $NNS link set st-e2e-n up
ip -n $CNS addr add 10.99.0.2/24 dev st-e2e-c
ip -n $CNS link set lo up
ip -n $CNS link set st-e2e-c up
ip -n $CNS route add default via 10.99.0.1
echo "client ns sysctls: $(ip netns exec $CNS sysctl -n net.ipv4.conf.all.rp_filter net.ipv4.conf.st-e2e-c.rp_filter net.ipv4.conf.all.src_valid_mark | tr '\n' ' ')"

mkdir -p "$DIR/excluded" "$DIR/normal" "$DIR/child" "$DIR/launcher" "$DIR/test"
for d in excluded normal child launcher; do
	install -m 755 "$APP_BIN" "$DIR/$d/app"
done
install -m 755 "$TEST_BIN" "$DIR/test/splittunnel.test"

ip netns exec $CNS "$DIR/launcher/app" launcher "$DIR/launcher.sock" >"$DIR/launcher.log" 2>&1 &
LAUNCHER_PID=$!
for _ in $(seq 50); do
	[ -S "$DIR/launcher.sock" ] && break
	sleep 0.1
done
set +e
ip netns exec $CNS env ST_E2E_DIR="$DIR" ST_E2E_NET_NS=/run/netns/$NNS \
	"$DIR/test/splittunnel.test" -test.run '^TestSplitTunnelRealKernelE2E$' -test.v -test.count 1 -test.timeout 5m "$@" 2>&1
rc=$?
echo "TEST EXIT CODE $rc"
echo "--- launcher log"
cat "$DIR/launcher.log"
exit $rc
