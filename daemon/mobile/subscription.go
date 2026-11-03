package mobile

// Ports apps/desktop/src/shared/cachedSubscription.ts: the last hub answer is
// kept so an unreachable hub neither locks out a paying user nor fails open.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

const keySubscription = "subscription"

const (
	maxOfflineGrace = 14 * 24 * time.Hour
	renewalGrace    = 3 * 24 * time.Hour
	futureSkew      = 24 * time.Hour
)

// hubSubscription is the hub's wire shape; Entitled is absent on older hubs.
type hubSubscription struct {
	Status    string  `json:"status"`
	Entitled  *bool   `json:"entitled,omitempty"`
	Renews    bool    `json:"renews"`
	ExpiresAt *string `json:"expiresAt"`
}

// cachedSubscription is what the store holds. Owner ties it to the session
// that fetched it, since the app and extension share one store.
type cachedSubscription struct {
	Subscription hubSubscription `json:"subscription"`
	CachedAt     int64           `json:"cachedAt"`
	Owner        string          `json:"owner"`
}

var subscriptionStatuses = map[string]bool{
	"trialing": true, "active": true, "past_due": true, "canceled": true,
	"unpaid": true, "incomplete": true, "none": true,
}

var entitledStatuses = map[string]bool{"trialing": true, "active": true}

// cacheOwner tags a cache with the account and device without storing the
// license itself a second time.
func cacheOwner(licenseKey, identityPub string) string {
	sum := sha256.Sum256([]byte("pangea-cache\x00" + licenseKey + "\x00" + identityPub))
	return hex.EncodeToString(sum[:16])
}

func validSubscription(s hubSubscription) bool {
	return subscriptionStatuses[s.Status]
}

// liveEntitlement trusts the hub's verdict, else the status alone.
func liveEntitlement(s hubSubscription) bool {
	if s.Entitled != nil {
		return *s.Entitled
	}
	return entitledStatuses[s.Status]
}

// cachedEntitlement is an exact port of desktop's: bounded benefit of the doubt
// for a renewal the hub could not confirm, and nothing past 14 days offline.
func cachedEntitlement(c cachedSubscription, now time.Time) bool {
	nowMs := now.UnixMilli()
	sub := c.Subscription
	if nowMs-c.CachedAt > maxOfflineGrace.Milliseconds() {
		return false
	}
	if sub.Entitled != nil && !*sub.Entitled {
		return false
	}
	if sub.ExpiresAt != nil && *sub.ExpiresAt != "" {
		if expiry, ok := parseJSDate(*sub.ExpiresAt); ok && expiry <= nowMs {
			deadline := expiry
			if sub.Renews {
				deadline += renewalGrace.Milliseconds()
			}
			if nowMs > deadline {
				return false
			}
		}
	}
	return liveEntitlement(sub)
}

// parseJSDate covers the ISO forms Date.parse accepts from the hub.
func parseJSDate(value string) (int64, bool) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UnixMilli(), true
		}
	}
	return 0, false
}

func encodeCachedSubscription(s hubSubscription, owner string, now time.Time) string {
	b, err := json.Marshal(cachedSubscription{Subscription: s, CachedAt: now.UnixMilli(), Owner: owner})
	if err != nil {
		return ""
	}
	return string(b)
}

// restoreCachedSubscription refuses a blob that is malformed, another
// session's, or stamped more than a day ahead, which a clock jump could pin.
func restoreCachedSubscription(raw, owner string, now time.Time) (cachedSubscription, bool) {
	var blob struct {
		Subscription json.RawMessage `json:"subscription"`
		CachedAt     *int64          `json:"cachedAt"`
		Owner        string          `json:"owner"`
	}
	if raw == "" || json.Unmarshal([]byte(raw), &blob) != nil || blob.CachedAt == nil {
		return cachedSubscription{}, false
	}
	if blob.Owner != owner || *blob.CachedAt > now.Add(futureSkew).UnixMilli() {
		return cachedSubscription{}, false
	}
	sub, ok := decodeStoredSubscription(blob.Subscription)
	if !ok {
		return cachedSubscription{}, false
	}
	return cachedSubscription{Subscription: sub, CachedAt: *blob.CachedAt, Owner: owner}, true
}

// decodeStoredSubscription ports isSubscriptionInfo: renews and expiresAt
// must be present with the right types, entitled may be absent.
func decodeStoredSubscription(raw json.RawMessage) (hubSubscription, bool) {
	var probe struct {
		Status    string          `json:"status"`
		Entitled  *bool           `json:"entitled"`
		Renews    *bool           `json:"renews"`
		ExpiresAt json.RawMessage `json:"expiresAt"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &probe) != nil || probe.Renews == nil || len(probe.ExpiresAt) == 0 {
		return hubSubscription{}, false
	}
	var expires *string
	if string(probe.ExpiresAt) != "null" {
		var value string
		if json.Unmarshal(probe.ExpiresAt, &value) != nil {
			return hubSubscription{}, false
		}
		expires = &value
	}
	sub := hubSubscription{Status: probe.Status, Entitled: probe.Entitled, Renews: *probe.Renews, ExpiresAt: expires}
	return sub, validSubscription(sub)
}

func liveSubscriptionInfo(s hubSubscription) subscriptionInfo {
	return subscriptionInfo{Status: s.Status, Renews: s.Renews, ExpiresAt: s.ExpiresAt, Entitled: liveEntitlement(s)}
}

func cachedSubscriptionInfo(c cachedSubscription, now time.Time) subscriptionInfo {
	s := c.Subscription
	return subscriptionInfo{Status: s.Status, Renews: s.Renews, ExpiresAt: s.ExpiresAt, Entitled: cachedEntitlement(c, now), Cached: true}
}
