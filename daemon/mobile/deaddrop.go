package mobile

// Verification for the dead-drop bootstrap file: a signed blob of replacement
// hub addresses. Ports apps/desktop/src/shared/deadDropBlob.ts and main/deadDrop.ts.

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

// deadDropKeys are raw Ed25519 verify keys, base64. The reserve exists so a
// compromised active key needs no emergency release.
type deadDropKeys struct {
	active  string
	reserve string
}

var productionDeadDropKeys = deadDropKeys{
	active:  "d1fIudP+7WehrFOqar8LxneSKuvBSQlIHsqKgQXTJFQ=",
	reserve: "btSYGcZsOJ+G1UkSYiowPrFnbRA3yt12QwMI7XEmpS0=",
}

// deadDropURLs are two addresses for one file: jsDelivr is a second way to
// it, not a second source of truth.
var deadDropURLs = []string{
	"https://raw.githubusercontent.com/pangeavpn/PangeaConfig/main/bootstrap-v1.json",
	"https://cdn.jsdelivr.net/gh/pangeavpn/PangeaConfig@main/bootstrap-v1.json",
}

const (
	deadDropMinInterval   = 15 * time.Minute
	deadDropPayloadV      = 1
	deadDropMaxBlobBytes  = 64 * 1024
	deadDropFetchTimeout  = 6 * time.Second
	ed25519SignatureBytes = 64
	// maxSafeInteger is JS Number.MAX_SAFE_INTEGER, the largest seq desktop accepts.
	maxSafeInteger = 1<<53 - 1
)

type deadDropPayload struct {
	Seq              int64
	HubIPs           []string
	FrontedEndpoints []string
}

// deadDropDue rate-limits the fetch so a restart loop never hammers the
// publishing hosts, and a backwards clock never locks fetching out.
func deadDropDue(lastAttemptMs, nowMs int64) bool {
	if lastAttemptMs <= 0 || lastAttemptMs > nowMs {
		return true
	}
	return nowMs-lastAttemptMs >= deadDropMinInterval.Milliseconds()
}

// verifyDeadDropBlob returns the payload only when every check passes, so a
// bad fetch is never worse than no fetch. It contributes addresses, nothing else.
func verifyDeadDropBlob(raw []byte, keys deadDropKeys, minSeq int64, now time.Time) (deadDropPayload, bool) {
	if len(raw) == 0 || len(raw) > deadDropMaxBlobBytes {
		return deadDropPayload{}, false
	}
	var envelope struct {
		Payload *string `json:"payload"`
		Sig     *string `json:"sig"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.Payload == nil || envelope.Sig == nil {
		return deadDropPayload{}, false
	}
	message, ok := decodeCanonicalBase64(*envelope.Payload)
	if !ok {
		return deadDropPayload{}, false
	}
	signature, ok := decodeCanonicalBase64(*envelope.Sig)
	if !ok || len(signature) != ed25519SignatureBytes {
		return deadDropPayload{}, false
	}
	if !verifiesUnder(keys.active, message, signature) && !verifiesUnder(keys.reserve, message, signature) {
		return deadDropPayload{}, false
	}
	return parseDeadDropBody(message, minSeq, now)
}

func parseDeadDropBody(message []byte, minSeq int64, now time.Time) (deadDropPayload, bool) {
	var body struct {
		V                json.Number `json:"v"`
		Seq              json.Number `json:"seq"`
		Expires          string      `json:"expires"`
		HubIPs           []any       `json:"hubIps"`
		FrontedEndpoints []any       `json:"frontedEndpoints"`
	}
	decoder := json.NewDecoder(bytes.NewReader(message))
	decoder.UseNumber()
	if decoder.Decode(&body) != nil {
		return deadDropPayload{}, false
	}
	if version, err := body.V.Int64(); err != nil || version != deadDropPayloadV {
		return deadDropPayload{}, false
	}
	seq, ok := acceptedSeq(body.Seq, minSeq)
	if !ok {
		return deadDropPayload{}, false
	}
	expires, err := time.Parse(time.RFC3339Nano, body.Expires)
	if err != nil || !expires.After(now) {
		return deadDropPayload{}, false
	}

	hubIPs := cleanList(body.HubIPs, func(entry string) (string, bool) {
		host := strings.TrimSpace(entry)
		return host, isIPv4Literal(host)
	})
	fronted := cleanList(body.FrontedEndpoints, normalizeFrontedEndpoint)
	// A verified blob that names nothing usable is not an instruction to forget
	// what the client already has.
	if len(hubIPs) == 0 && len(fronted) == 0 {
		return deadDropPayload{}, false
	}
	return deadDropPayload{Seq: seq, HubIPs: hubIPs, FrontedEndpoints: fronted}, true
}

// acceptedSeq must beat every blob already accepted, which is what makes a
// replayed older file harmless.
func acceptedSeq(value json.Number, minSeq int64) (int64, bool) {
	seq, err := value.Int64()
	if err != nil || seq < 0 || seq > maxSafeInteger || seq <= minSeq {
		return 0, false
	}
	return seq, true
}

func cleanList(values []any, normalize func(string) (string, bool)) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		entry, isString := value.(string)
		if !isString {
			continue
		}
		if normalized, ok := normalize(entry); ok && !containsString(out, normalized) {
			out = append(out, normalized)
		}
	}
	return out
}

// decodeCanonicalBase64 refuses any encoding but the canonical one, so a
// mangled variant cannot ride along with a valid signature.
func decodeCanonicalBase64(value string) ([]byte, bool) {
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(decoded) == 0 || base64.StdEncoding.EncodeToString(decoded) != value {
		return nil, false
	}
	return decoded, true
}

func verifiesUnder(keyB64 string, message, signature []byte) bool {
	key, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(key), message, signature)
}
