package httpx

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

type logLevel int

const (
	levelError logLevel = iota
	levelInfo
	levelDebug
)

var (
	logMu     sync.Mutex
	logWriter io.Writer = os.Stderr
	current             = levelInfo
)

func SetLogLevel(s string) {
	logMu.Lock()
	defer logMu.Unlock()
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		current = levelDebug
	case "error":
		current = levelError
	default:
		current = levelInfo
	}
}

func SetLogOutput(w io.Writer) func() {
	logMu.Lock()
	old := logWriter
	if w == nil {
		logWriter = io.Discard
	} else {
		logWriter = w
	}
	logMu.Unlock()
	return func() {
		logMu.Lock()
		logWriter = old
		logMu.Unlock()
	}
}

func LogOK(provider, model, route string) {
	var b strings.Builder
	b.WriteString("fob  200")
	if provider != "" {
		b.WriteString("  ")
		b.WriteString(provider)
		if model != "" {
			b.WriteByte('/')
			b.WriteString(model)
		}
	}
	if route != "" {
		b.WriteString("  ")
		b.WriteString(route)
	}
	logln(levelDebug, b.String())
}

func logln(min logLevel, line string) {
	logMu.Lock()
	defer logMu.Unlock()
	if current < min || logWriter == nil {
		return
	}
	fmt.Fprintln(logWriter, line)
}
