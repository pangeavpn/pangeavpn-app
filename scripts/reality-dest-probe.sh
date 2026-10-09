#!/usr/bin/env bash
# Probes candidate REALITY cover destinations for the properties the handshake
# needs. Run it ON A NODE: the node is what dials the dest, so its vantage is
# the one that decides. Running it on a client only sanity-checks the domain.
set -u

CANDIDATES_DEFAULT="
www.apple.com
swdist.apple.com
gdmf.apple.com
configuration.apple.com
www.microsoft.com
www.bing.com
dl.google.com
storage.googleapis.com
cdn.jsdelivr.net
aws.amazon.com
s3.amazonaws.com
www.cloudflare.com
one.one.one.one
"

ATTEMPTS=2
TIMEOUT=10
PORT=443
VERBOSE=0

usage() {
  cat <<'USAGE'
Usage: reality-dest-probe.sh [options] [host ...]

Options:
  -f FILE     read candidate hosts from FILE (one per line, # comments ok)
  -n N        attempts per host before declaring failure (default 2)
  -t SECS     per-attempt timeout (default 10)
  -p PORT     port to probe (default 443)
  -v          print the raw openssl output for each failure
  -h          this help

With no hosts and no -f, a built-in candidate list is used.

A dest PASSES only if all of these hold:
  * TLS 1.3 negotiated
  * X25519 accepted as the key-exchange group
  * h2 offered via ALPN
  * certificate chain verifies (code 0)
  * no client certificate requested
USAGE
}

while getopts "f:n:t:p:vh" opt; do
  case "$opt" in
    f) CAND_FILE="$OPTARG" ;;
    n) ATTEMPTS="$OPTARG" ;;
    t) TIMEOUT="$OPTARG" ;;
    p) PORT="$OPTARG" ;;
    v) VERBOSE=1 ;;
    h) usage; exit 0 ;;
    *) usage; exit 2 ;;
  esac
done
shift $((OPTIND - 1))

command -v openssl >/dev/null 2>&1 || { echo "openssl not found" >&2; exit 1; }

now_ms() {
  if perl -MTime::HiRes -e 1 >/dev/null 2>&1; then
    perl -MTime::HiRes=time -e 'printf "%.0f", time()*1000'
  else
    echo $(( $(date +%s) * 1000 ))
  fi
}

# timeout(1) is absent on stock macOS; fall back to running bare.
run_openssl() {
  host="$1"
  if command -v timeout >/dev/null 2>&1; then
    echo | timeout "$TIMEOUT" openssl s_client -connect "$host:$PORT" \
      -servername "$host" -tls1_3 -alpn h2 -groups X25519 2>&1
  else
    echo | openssl s_client -connect "$host:$PORT" \
      -servername "$host" -tls1_3 -alpn h2 -groups X25519 2>&1
  fi
}

field() { echo "$1" | grep -m1 "$2" | cut -d: -f2- | sed 's/^ *//'; }

probe_once() {
  host="$1"
  start=$(now_ms)
  raw=$(run_openssl "$host" | tr -d '\000')
  end=$(now_ms)
  ms=$((end - start))

  proto=$(echo "$raw" | grep -m1 "^Protocol *:" | cut -d: -f2- | tr -d ' ')
  tempkey=$(field "$raw" "Peer Temp Key")
  alpn=$(field "$raw" "ALPN protocol")
  verify=$(echo "$raw" | grep -m1 "Verify return code" | cut -d: -f2- | sed 's/^ *//')
  cn=$(echo "$raw" | grep -m1 "^ 0 s:" | sed 's/.*CN *= *//')
  clientcert=$(echo "$raw" | grep -c "Acceptable client certificate CA names")

  # The SNI must be in the SAN list, not merely the CN: plenty of good dests
  # present a cert whose CN is a sibling name (swdist -> CN swcdn).
  san=$(echo "$raw" | sed -n '/-BEGIN CERTIFICATE-/,/-END CERTIFICATE-/p' \
    | openssl x509 -noout -ext subjectAltName 2>/dev/null | tr ',' '\n' \
    | sed -n 's/.*DNS://p' | tr -d ' ')
  sanok=0
  for n in $san; do
    case "$host" in
      $n) sanok=1; break ;;
    esac
    [ "$n" = "$host" ] && { sanok=1; break; }
  done

  RESULT_MS="$ms"
  RESULT_CN="${cn:-?}"
  RESULT_RAW="$raw"

  reasons=""
  case "$proto" in TLSv1.3) ;; *) reasons="$reasons no-tls13" ;; esac
  case "$tempkey" in X25519*) ;; *) reasons="$reasons no-x25519" ;; esac
  case "$alpn" in h2) ;; *) reasons="$reasons no-h2" ;; esac
  case "$verify" in "0 (ok)") ;; *) reasons="$reasons badcert" ;; esac
  [ "$sanok" -eq 0 ] && reasons="$reasons sni-not-in-san"
  [ "$clientcert" -gt 0 ] && reasons="$reasons wants-client-cert"

  RESULT_REASONS="$reasons"
  [ -z "$reasons" ]
}

probe() {
  host="$1"
  i=1
  while [ "$i" -le "$ATTEMPTS" ]; do
    if probe_once "$host"; then
      printf "%-30s %-6s %6sms  %s\n" "$host" "PASS" "$RESULT_MS" "$RESULT_CN"
      return 0
    fi
    i=$((i + 1))
  done
  printf "%-30s %-6s %6sms  %s\n" "$host" "FAIL" "$RESULT_MS" "${RESULT_REASONS# }"
  [ "$VERBOSE" -eq 1 ] && echo "$RESULT_RAW" | sed 's/^/    | /'
  return 1
}

if [ "$#" -gt 0 ]; then
  hosts="$*"
elif [ -n "${CAND_FILE:-}" ]; then
  hosts=$(grep -v '^[[:space:]]*#' "$CAND_FILE" | grep -v '^[[:space:]]*$')
else
  hosts="$CANDIDATES_DEFAULT"
fi

echo "REALITY dest probe  port=$PORT  attempts=$ATTEMPTS  timeout=${TIMEOUT}s"
echo "vantage: $(hostname 2>/dev/null || echo unknown)"
echo
printf "%-30s %-6s %8s  %s\n" HOST VERDICT TIME "CERT_CN / FAIL_REASONS"
printf "%-30s %-6s %8s  %s\n" "------------------------------" "------" "--------" "----------------------"

passed=0
failed=0
for h in $hosts; do
  if probe "$h"; then passed=$((passed + 1)); else failed=$((failed + 1)); fi
done

echo
echo "$passed passed, $failed failed"
echo
echo "A PASS means the dest is technically usable. Still judge it on:"
echo "  * plausibility  - would a machine on this network really talk to it, repeatedly, for hours?"
echo "  * traffic shape - the tunnel is bulk bidirectional transfer; pick a dest whose real"
echo "                    traffic looks like that (CDN, updates, storage) rather than a"
echo "                    request/response API like a DoH resolver."
echo "  * inspection    - domains that break pinned enterprise clients when decrypted are"
echo "                    usually on the corporate TLS-inspection bypass list."
echo "  * locality      - lowest time from THIS node, so the disguise's RTT matches."
