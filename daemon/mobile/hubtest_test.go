package mobile

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHubTestResultJSON(t *testing.T) {
	cases := []struct {
		in   hubTestResult
		want string
	}{
		{hubTestResult{Method: "reality", Unavailable: "busy"}, `{"method":"reality","ok":false,"ms":0,"unavailable":"busy"}`},
		{hubTestResult{Method: "directIp", OK: true, MS: 42, Detail: "203.0.113.4"}, `{"method":"directIp","ok":true,"ms":42,"detail":"203.0.113.4"}`},
		{hubTestResult{Method: "fronted", MS: 7}, `{"method":"fronted","ok":false,"ms":7}`},
	}
	for _, c := range cases {
		got, err := encodeHubTestResult(c.in)
		if err != nil || got != c.want {
			t.Errorf("got %s, %v, want %s", got, err, c.want)
		}
	}
}

func TestHubStatusJSONNullsTheUnknown(t *testing.T) {
	methods := defaultHubMethods()
	none := encodeHubStatus(hubStatusFor(methods, "", ""))
	want := `{"methods":{"directIp":true,"reality":true,"shadowsocks":true,"fronted":true,"normal":false,"rev":2},"active":null,"detail":null}`
	if none != want {
		t.Fatalf("got %s, want %s", none, want)
	}
	bare := encodeHubStatus(hubStatusFor(methods, "normal", ""))
	if !strings.HasSuffix(bare, `"active":"normal","detail":null}`) {
		t.Fatalf("got %s, want active normal with a null detail", bare)
	}
	full := encodeHubStatus(hubStatusFor(methods, "reality", "192.0.2.7"))
	if !strings.HasSuffix(full, `"active":"reality","detail":"192.0.2.7"}`) {
		t.Fatalf("got %s, want active reality with its node", full)
	}
}

func TestRunHubTestRejectsUnknownMethods(t *testing.T) {
	var busy atomic.Bool
	probed := false
	_, err := runHubTest("carrier-pigeon", &busy, func(string) hubTestResult { probed = true; return hubTestResult{} })
	if err == nil || err.Error() != `unknown hub method "carrier-pigeon"` {
		t.Fatalf("got %v, want an unknown method error", err)
	}
	if probed || busy.Load() {
		t.Fatalf("probed %v, busy %v, want neither", probed, busy.Load())
	}
}

func TestRunHubTestReportsBusyWithoutProbing(t *testing.T) {
	var busy atomic.Bool
	busy.Store(true)
	got, err := runHubTest("reality", &busy, func(string) hubTestResult {
		t.Fatal("probed while another test ran")
		return hubTestResult{}
	})
	if err != nil || got != `{"method":"reality","ok":false,"ms":0,"unavailable":"busy"}` {
		t.Fatalf("got %s, %v, want busy", got, err)
	}
	if !busy.Load() {
		t.Fatal("a busy answer released the other test's guard")
	}
}

func TestRunHubTestTimesTheProbeAndReleasesTheGuard(t *testing.T) {
	var busy atomic.Bool
	got, err := runHubTest("fronted", &busy, func(method string) hubTestResult {
		if !busy.Load() {
			t.Error("probe ran without holding the guard")
		}
		time.Sleep(20 * time.Millisecond)
		return hubTestResult{Method: "wrong", OK: true, MS: -1, Detail: "relay.example"}
	})
	if err != nil {
		t.Fatalf("runHubTest: %v", err)
	}
	if !strings.HasPrefix(got, `{"method":"fronted","ok":true,"ms":`) || !strings.HasSuffix(got, `,"detail":"relay.example"}`) {
		t.Fatalf("got %s, want the method and detail kept", got)
	}
	if strings.Contains(got, `"ms":-1`) || strings.Contains(got, `"ms":0,`) {
		t.Fatalf("got %s, want the measured round trip", got)
	}
	if busy.Load() {
		t.Fatal("guard still held after the probe")
	}
}
