package state

import (
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

// ProcessStartedAt is when this process began, in unix milliseconds. A report
// that carries it can tell a restart from a long-running daemon without guessing.
var ProcessStartedAt = time.Now().UnixMilli()

var (
	buildOnce sync.Once
	buildInfo DaemonBuild
)

// Build reports the daemon binary's identity. Go embeds the VCS revision when
// the build ran inside a checkout, so this names the commit the daemon really is.
func Build() DaemonBuild {
	buildOnce.Do(func() {
		info, ok := debug.ReadBuildInfo()
		if !ok {
			return
		}
		if version := strings.TrimSpace(info.Main.Version); version != "(devel)" {
			buildInfo.Version = version
		}
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				buildInfo.Revision = strings.TrimSpace(setting.Value)
			case "vcs.time":
				buildInfo.Time = strings.TrimSpace(setting.Value)
			case "vcs.modified":
				buildInfo.Modified = setting.Value == "true"
			}
		}
	})
	return buildInfo
}
