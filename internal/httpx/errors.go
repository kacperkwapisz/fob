package httpx

import "net/http"

func OpenAIUnauthorized(w http.ResponseWriter) {
	JSON(w, http.StatusUnauthorized, map[string]any{
		"error": map[string]any{"message": "invalid api key", "type": "invalid_request_error"},
	})
}

func ClaudeUnauthorized(w http.ResponseWriter) {
	JSON(w, http.StatusUnauthorized, map[string]any{
		"error": map[string]any{"type": "authentication_error", "message": "invalid api key"},
	})
}

func PanelUnauthorized(w http.ResponseWriter, message string) {
	if message == "" {
		message = "unauthorized"
	}
	JSON(w, http.StatusUnauthorized, map[string]any{"error": message})
}

func OpenAIError(typ, message string) map[string]any {
	return map[string]any{"error": map[string]any{"message": message, "type": typ, "code": typ}}
}
