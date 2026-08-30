package httpx

import "net/url"

func parseQuery(text string) (url.Values, error) {
	return url.ParseQuery(text)
}

func FormString(body any, key string) string {
	switch t := body.(type) {
	case map[string]string:
		return t[key]
	case map[string]any:
		if v, ok := t[key].(string); ok {
			return v
		}
	}
	return ""
}

func JSONField(body any, key string) string {
	m, ok := body.(map[string]any)
	if !ok {
		return ""
	}
	v, ok := m[key].(string)
	if !ok {
		return ""
	}
	return v
}
