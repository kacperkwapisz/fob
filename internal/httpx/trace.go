package httpx

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	TraceOff    = "off"
	TraceErrors = "errors"
	TraceAlways = "always"

	SettingLogTrace = "log.trace"
)

type Trace struct {
	mu    sync.Mutex
	lines []string
}

type traceCtxKey struct{}

var traceMode atomic.Value

func init() {
	traceMode.Store(TraceOff)
}

func SetTraceMode(s string) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case TraceAlways:
		traceMode.Store(TraceAlways)
	case TraceErrors:
		traceMode.Store(TraceErrors)
	default:
		traceMode.Store(TraceOff)
	}
}

func TraceMode() string {
	if v, ok := traceMode.Load().(string); ok {
		return v
	}
	return TraceOff
}

func NewTrace() *Trace { return &Trace{} }

func WithTrace(ctx context.Context, tr *Trace) context.Context {
	if tr == nil {
		return ctx
	}
	return context.WithValue(ctx, traceCtxKey{}, tr)
}

func TraceFrom(ctx context.Context) *Trace {
	if ctx == nil {
		return nil
	}
	tr, _ := ctx.Value(traceCtxKey{}).(*Trace)
	return tr
}

func (t *Trace) Add(format string, args ...any) {
	if t == nil {
		return
	}
	line := strings.TrimSpace(fmt.Sprintf(format, args...))
	if line == "" {
		return
	}
	t.mu.Lock()
	t.lines = append(t.lines, line)
	t.mu.Unlock()
}

func (t *Trace) Dump(reason string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	lines := append([]string(nil), t.lines...)
	t.mu.Unlock()
	if len(lines) == 0 && reason == "" {
		return
	}
	head := "fob trace"
	if reason != "" {
		head += "  " + reason
	}
	logln(levelInfo, head)
	for _, line := range lines {
		logln(levelInfo, "  "+line)
	}
}

func ShouldTrace() bool {
	m := TraceMode()
	return m == TraceAlways || m == TraceErrors
}

func DumpTrace(ctx context.Context, failed bool, reason string) {
	m := TraceMode()
	if m == TraceOff {
		return
	}
	if m == TraceErrors && !failed {
		return
	}
	if tr := TraceFrom(ctx); tr != nil {
		tr.Dump(reason)
	}
}
