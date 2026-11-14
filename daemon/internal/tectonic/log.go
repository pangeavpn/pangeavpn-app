package tectonic

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// newLogger forwards the client's Info and above to the daemon's log store.
// Debug covers every lane dial and retirement, too many for the UI's log.
func newLogger(logs *state.LogStore) *slog.Logger {
	if logs == nil {
		return slog.New(slog.DiscardHandler)
	}
	return slog.New(&logHandler{logs: logs})
}

type logHandler struct {
	logs  *state.LogStore
	attrs []slog.Attr
	group string
}

func (h *logHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelInfo
}

func (h *logHandler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	b.WriteString("tectonic: ")
	b.WriteString(r.Message)
	write := func(a slog.Attr) bool {
		key := a.Key
		if h.group != "" {
			key = h.group + "." + key
		}
		fmt.Fprintf(&b, " %s=%v", key, a.Value.Resolve())
		return true
	}
	for _, a := range h.attrs {
		write(a)
	}
	r.Attrs(write)
	h.logs.Add(levelFor(r.Level), state.SourceTectonic, b.String())
	return nil
}

func (h *logHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := *h
	out.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &out
}

func (h *logHandler) WithGroup(name string) slog.Handler {
	out := *h
	if out.group != "" {
		name = out.group + "." + name
	}
	out.group = name
	return &out
}

func levelFor(l slog.Level) state.LogLevel {
	switch {
	case l >= slog.LevelError:
		return state.LogError
	case l >= slog.LevelWarn:
		return state.LogWarn
	default:
		return state.LogInfo
	}
}
