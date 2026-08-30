package translate

import "encoding/json"

func marshalJSON(v any) ([]byte, error) { return json.Marshal(v) }

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}

func nowUnix() int64 { return nowUnixFn() }

var nowUnixFn = func() int64 {
	return unixNow()
}
