package httpx

import "net/http"

func InboundHeaders(r *http.Request) map[string]string {
	out := map[string]string{}
	for k, vs := range r.Header {
		if len(vs) > 0 {
			out[k] = vs[0]
		}
	}
	return out
}
