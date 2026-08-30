package oauth

import (
	"net/url"
	"strings"
)

type ParsedCallback struct {
	Code             string
	State            string
	Error            string
	ErrorDescription string
}

func ParseCallback(raw string) ParsedCallback {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ParsedCallback{}
	}
	if !strings.Contains(value, "://") && !strings.Contains(value, "?") && !strings.Contains(value, "&") && !strings.Contains(value, "=") {
		return ParsedCallback{Code: value}
	}
	var u *url.URL
	var err error
	if strings.HasPrefix(value, "http") {
		u, err = url.Parse(value)
	} else {
		u, err = url.Parse("http://localhost/callback?" + strings.TrimPrefix(value, "?"))
	}
	if err != nil {
		return ParsedCallback{Code: value}
	}
	q := u.Query()
	out := ParsedCallback{}
	if v := q.Get("code"); v != "" {
		out.Code = v
	}
	if v := q.Get("state"); v != "" {
		out.State = v
	}
	if v := q.Get("error"); v != "" {
		out.Error = v
	}
	if v := q.Get("error_description"); v != "" {
		out.ErrorDescription = v
	}
	return out
}
