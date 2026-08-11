package mobile

import "testing"

func TestDefaultHubMethodsKeepCleartextOff(t *testing.T) {
	got := defaultHubMethods()
	if !got.DirectIP || !got.Shadowsocks || !got.Fronted {
		t.Fatalf("got %+v, want the private paths on", got)
	}
	if got.Normal {
		t.Fatal("normal names the hub in cleartext and must default off")
	}
}

func TestEnabledFollowsAttemptOrder(t *testing.T) {
	methods := hubMethods{DirectIP: true, Shadowsocks: false, Fronted: true, Normal: true}
	got := methods.enabled()
	want := []string{"directIp", "fronted", "normal"}
	if !sameStrings(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// Disabling everything would leave no way to reach the hub at all.
func TestApplyHubMethodRefusesToDisableTheLastOne(t *testing.T) {
	only := hubMethods{DirectIP: true}
	got, applied := applyHubMethod(only, "directIp", false)
	if applied {
		t.Fatal("disabling the last method should be refused")
	}
	if !got.DirectIP {
		t.Fatalf("the refused switch must not be applied, got %+v", got)
	}
}

func TestApplyHubMethodTogglesWhenOthersRemain(t *testing.T) {
	methods := hubMethods{DirectIP: true, Fronted: true}
	got, applied := applyHubMethod(methods, "directIp", false)
	if !applied {
		t.Fatal("should apply while another method remains")
	}
	if got.DirectIP || !got.Fronted {
		t.Fatalf("got %+v, want only fronted", got)
	}
}

func TestApplyHubMethodIsIdempotent(t *testing.T) {
	methods := hubMethods{DirectIP: true}
	if _, applied := applyHubMethod(methods, "directIp", true); !applied {
		t.Fatal("setting a method to its current value should report applied")
	}
}

func TestApplyHubMethodRejectsUnknown(t *testing.T) {
	if _, applied := applyHubMethod(defaultHubMethods(), "carrier-pigeon", true); applied {
		t.Fatal("unknown method should not apply")
	}
}

// A blob written before a default flipped looks identical to a deliberate
// choice, so the rev re-applies the change exactly once.
func TestNormalizeReappliesChangedDefaultsOnce(t *testing.T) {
	stored := hubMethods{DirectIP: true, Shadowsocks: false, Fronted: false, Rev: 0}
	got := stored.normalize()
	if !got.Shadowsocks || !got.Fronted {
		t.Fatalf("rev 1 defaults should be re-applied, got %+v", got)
	}
	if got.Rev != hubMethodsRev {
		t.Fatalf("rev %d, want %d", got.Rev, hubMethodsRev)
	}

	// At the current rev the stored choice wins.
	deliberate := hubMethods{DirectIP: true, Shadowsocks: false, Fronted: false, Rev: hubMethodsRev}
	if again := deliberate.normalize(); again.Shadowsocks || again.Fronted {
		t.Fatalf("a current-rev choice must be preserved, got %+v", again)
	}
}

func TestNormalizeGuaranteesOneEnabledMethod(t *testing.T) {
	got := hubMethods{Rev: hubMethodsRev}.normalize()
	if len(got.enabled()) == 0 {
		t.Fatal("normalize must leave at least one method on")
	}
	if !got.DirectIP {
		t.Fatalf("directIp is the safe fallback, got %+v", got)
	}
}
