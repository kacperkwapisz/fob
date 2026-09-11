package provider

import (
	"io"
	"net/http"

	"github.com/kacperkwapisz/fob/internal/httpx"
)

func FailFromResponse(res *http.Response) ExecuteResult {
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	msg, body := httpx.SanitizeUpstream(res.StatusCode, raw)
	if body == nil && msg != "" {
		body = httpx.OpenAIError(httpx.TypeFor(res.StatusCode, ""), msg)
	}
	return ExecuteResult{
		OK: false, Status: res.StatusCode, Retryable: IsRetryableStatus(res.StatusCode),
		Body: body, Message: msg,
	}
}

func FailFromErr(err error) ExecuteResult {
	status, _, msg := httpx.ClassifyErr(err)
	return ExecuteResult{
		OK: false, Status: status, Retryable: IsRetryableStatus(status),
		Message: msg,
	}
}
