package oauth

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}

const openaiAuthClaim = "https://api.openai.com/auth"

func chatgptAccountID(tokens ...string) string {
	for _, token := range tokens {
		payload := decodeJWT(token)
		if id := asString(payload["chatgpt_account_id"]); id != "" {
			return id
		}
		if id := asString(asMap(payload[openaiAuthClaim])["chatgpt_account_id"]); id != "" {
			return id
		}
	}
	return ""
}
