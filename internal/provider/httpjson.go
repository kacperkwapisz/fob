package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

func postJSONWith(ctx context.Context, client *http.Client, rawURL string, body any, headers map[string]string) (*http.Response, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}
	return client.Do(req)
}

func ParseSSE(r io.Reader, out chan<- any) {
	defer close(out)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	var block strings.Builder
	flush := func() {
		s := strings.TrimRight(block.String(), "\n")
		block.Reset()
		if s == "" {
			return
		}
		if ev := sseEvent(s); ev != nil {
			out <- ev
		}
	}
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			flush()
			continue
		}
		block.WriteString(line)
		block.WriteByte('\n')
	}
	flush()
}

func sseEvent(block string) any {
	var dataLines []string
	var event string
	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimLeft(line[5:], " \t"))
		} else if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(line[6:])
		}
	}
	data := strings.Join(dataLines, "\n")
	if data == "" || data == "[DONE]" {
		if event != "" {
			return map[string]any{"type": event, "data": data}
		}
		return nil
	}
	var jsonVal any
	if json.Unmarshal([]byte(data), &jsonVal) != nil {
		typ := event
		if typ == "" {
			typ = "message"
		}
		return map[string]any{"type": typ, "data": data}
	}
	if event != "" {
		if m, ok := jsonVal.(map[string]any); ok {
			if _, has := m["type"]; !has {
				m["type"] = event
				return m
			}
		}
	}
	return jsonVal
}
