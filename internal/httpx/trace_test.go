package httpx

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestTraceDumpModes(t *testing.T) {
	var buf bytes.Buffer
	defer SetLogOutput(&buf)()
	SetLogLevel("info")
	defer SetTraceMode(TraceOff)

	tr := NewTrace()
	ctx := WithTrace(context.Background(), tr)
	tr.Add("cursor wire=%s", "gpt-5.6-sol-medium")

	SetTraceMode(TraceOff)
	DumpTrace(ctx, true, "failed")
	if buf.Len() != 0 {
		t.Fatalf("off dumped %q", buf.String())
	}

	SetTraceMode(TraceErrors)
	DumpTrace(ctx, false, "ok")
	if buf.Len() != 0 {
		t.Fatalf("errors dumped success %q", buf.String())
	}
	DumpTrace(ctx, true, "unfinished stream")
	got := buf.String()
	if !strings.Contains(got, "fob trace  unfinished stream") || !strings.Contains(got, "cursor wire=gpt-5.6-sol-medium") {
		t.Fatalf("errors dump %q", got)
	}

	buf.Reset()
	SetTraceMode(TraceAlways)
	DumpTrace(ctx, false, "ok")
	if !strings.Contains(buf.String(), "fob trace  ok") {
		t.Fatalf("always dump %q", buf.String())
	}
}
