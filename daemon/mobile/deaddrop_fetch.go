//go:build android

package mobile

// Fetches the dead-drop file over ordinary HTTPS with certificate validation
// on: these are real hosts, unlike the direct-IP hub path.

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

const (
	keyDeadDropSeq     = "deadDropSeq"
	keyDeadDropAttempt = "deadDropLastAttempt"
)

// reseedFromDeadDrop folds a verified blob into the caches the search reads,
// reporting whether anything changed. An unreachable drop is a no-op.
func reseedFromDeadDrop(logs *state.LogStore) bool {
	now := time.Now()
	if !deadDropDue(storedInt(keyDeadDropAttempt), now.UnixMilli()) {
		logAdd(logs, state.LogInfo, "dead drop rate limited, skipping")
		return false
	}
	setStoredValue(keyDeadDropAttempt, strconv.FormatInt(now.UnixMilli(), 10))

	payload, source, ok := fetchDeadDropPayload(storedInt(keyDeadDropSeq), now)
	if !ok {
		logAdd(logs, state.LogInfo, "dead drop had nothing usable")
		return false
	}
	logAdd(logs, state.LogInfo, fmt.Sprintf("dead drop seq %d from %s", payload.Seq, source))
	setStoredValue(keyDeadDropSeq, strconv.FormatInt(payload.Seq, 10))

	changed := false
	if merged := mergeFrontedEndpoints(loadFrontedEndpoints(), payload.FrontedEndpoints); merged != nil {
		saveFrontedEndpoints(merged)
		changed = true
	}
	// Only one hub IP is cached at a time, so take the first the blob names.
	if len(payload.HubIPs) > 0 && payload.HubIPs[0] != storedValue(keyHubIP) {
		setStoredValue(keyHubIP, payload.HubIPs[0])
		changed = true
	}
	return changed
}

// fetchDeadDropPayload returns the first published file that passes every
// check; one bad mirror never ends the search.
func fetchDeadDropPayload(minSeq int64, now time.Time) (deadDropPayload, string, bool) {
	client := &http.Client{
		Timeout:   deadDropFetchTimeout,
		Transport: &http.Transport{DialContext: protectedDialer(deadDropFetchTimeout).DialContext},
	}
	for _, source := range deadDropURLs {
		if !strings.HasPrefix(source, "https://") {
			continue
		}
		body, ok := getSmallBody(client, source)
		if !ok {
			continue
		}
		if payload, ok := verifyDeadDropBlob(body, productionDeadDropKeys, minSeq, now); ok {
			return payload, source, true
		}
	}
	return deadDropPayload{}, "", false
}

func getSmallBody(client *http.Client, source string) ([]byte, bool) {
	resp, err := client.Get(source)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, deadDropMaxBlobBytes+1))
	if err != nil || len(body) > deadDropMaxBlobBytes {
		return nil, false
	}
	return body, true
}

func storedInt(key string) int64 {
	value, err := strconv.ParseInt(strings.TrimSpace(storedValue(key)), 10, 64)
	if err != nil {
		return 0
	}
	return value
}
