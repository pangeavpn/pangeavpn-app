package mobile

// Shapes for testing one hub method and reporting the route in use. Ports
// HubMethodTestResult and HubStatus in apps/desktop/src/shared/hubMethods.ts.

import (
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"
)

// Why a method had nothing to try, so the host can say more than "failed".
const (
	hubTestNoAddress     = "noAddress"
	hubTestNoCredentials = "noCredentials"
	hubTestNoRelay       = "noRelay"
	hubTestBusyReason    = "busy"
)

type hubTestResult struct {
	Method string `json:"method"`
	OK     bool   `json:"ok"`
	MS     int64  `json:"ms"`
	// Detail is the address, relay or node the probe reached.
	Detail      string `json:"detail,omitempty"`
	Unavailable string `json:"unavailable,omitempty"`
}

type hubStatusJSON struct {
	Methods hubMethods `json:"methods"`
	// Active and Detail are null while no route is resolved.
	Active *string `json:"active"`
	Detail *string `json:"detail"`
}

func encodeHubTestResult(r hubTestResult) (string, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return "", fmt.Errorf("marshal hub test: %w", err)
	}
	return string(b), nil
}

// hubStatusFor reports kind as the active method, "" meaning none.
func hubStatusFor(methods hubMethods, kind, detail string) hubStatusJSON {
	status := hubStatusJSON{Methods: methods}
	if kind != "" {
		status.Active = &kind
		if detail != "" {
			status.Detail = &detail
		}
	}
	return status
}

func encodeHubStatus(status hubStatusJSON) string {
	b, err := json.Marshal(status)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// runHubTest answers busy while another test holds the guard, and stamps the
// method and round trip itself so a probe cannot misreport either.
func runHubTest(method string, busy *atomic.Bool, probe func(string) hubTestResult) (string, error) {
	if !isHubMethod(method) {
		return "", fmt.Errorf("unknown hub method %q", method)
	}
	if !busy.CompareAndSwap(false, true) {
		return encodeHubTestResult(hubTestResult{Method: method, Unavailable: hubTestBusyReason})
	}
	defer busy.Store(false)
	start := time.Now()
	result := probe(method)
	result.Method = method
	result.MS = time.Since(start).Milliseconds()
	return encodeHubTestResult(result)
}
