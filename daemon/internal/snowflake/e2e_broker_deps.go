//go:build transport_e2e

package snowflake

// The E2E test builds the Snowflake broker as a subprocess. These are the
// broker's dependencies that the daemon does not otherwise import; blank
// imports here keep them in go.mod/go.sum so `go mod tidy` cannot prune them
// out from under the broker build.
import (
	_ "github.com/clarkduvall/hyperloglog"
	_ "gitlab.torproject.org/tpo/anti-censorship/geoip"
)
