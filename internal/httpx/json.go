package httpx

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

const BodyLimitBytes = 32_000_000

var forbiddenJSONKeys = map[string]struct{}{
	"__proto__":   {},
	"constructor": {},
	"prototype":   {},
}

func writeJSON(w io.Writer, v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return err
	}
	out := bytes.TrimRight(buf.Bytes(), "\n")
	_, err := w.Write(out)
	return err
}

func ParseBody(r *http.Request) (any, error) {
	ct := strings.ToLower(r.Header.Get("content-type"))
	limited := io.LimitReader(r.Body, BodyLimitBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(raw) > BodyLimitBytes {
		return nil, &StatusError{Status: 413, Message: "payload too large"}
	}
	if strings.HasPrefix(ct, "application/x-www-form-urlencoded") {
		return parseForm(string(raw)), nil
	}
	if len(raw) == 0 {
		return nil, nil
	}
	if ct == "" || strings.HasPrefix(ct, "application/json") || strings.HasPrefix(ct, "application/ld+json") {
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, &StatusError{Status: 400, Message: "Request body is not valid JSON."}
		}
		if err := rejectProtoKeys(v); err != nil {
			return nil, err
		}
		return v, nil
	}
	return string(raw), nil
}

func parseForm(text string) map[string]string {
	out := map[string]string{}
	values, err := parseQuery(text)
	if err != nil {
		return out
	}
	for k, vs := range values {
		if k == "__proto__" || k == "constructor" || k == "prototype" {
			continue
		}
		if len(vs) > 0 {
			out[k] = vs[0]
		}
	}
	return out
}

func rejectProtoKeys(v any) error {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			if _, bad := forbiddenJSONKeys[k]; bad {
				return &StatusError{Status: 400, Message: `Refusing body containing dangerous key "` + k + `".`}
			}
			if err := rejectProtoKeys(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range t {
			if err := rejectProtoKeys(child); err != nil {
				return err
			}
		}
	}
	return nil
}

type StatusError struct {
	Status  int
	Message string
}

func (e *StatusError) Error() string { return e.Message }
