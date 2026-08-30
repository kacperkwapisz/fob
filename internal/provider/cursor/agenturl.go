package cursor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/kacperkwapisz/fob/internal/httpx"
)

var agentHostRe = regexp.MustCompile(`(?i)^([a-z0-9-]+\.)+cursor\.sh$`)

func IsAllowedAgentHost(hostname string) bool {
	return agentHostRe.MatchString(hostname)
}

func NormalizeAgentRunOrigin(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if !strings.HasPrefix(strings.ToLower(trimmed), "http://") && !strings.HasPrefix(strings.ToLower(trimmed), "https://") {
		trimmed = "https://" + trimmed
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Scheme != "https" || u.User != nil || !IsAllowedAgentHost(u.Hostname()) {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

var (
	resolved = map[string]string{}
	inflight = map[string]chan struct{}{}
	resolveM sync.Mutex
)

func ResolveAgentURL(ctx context.Context, accessToken string, kind ClientKind) (string, error) {
	if override := strings.TrimSpace(os.Getenv("CURSOR_AGENT_URL")); override != "" {
		n := NormalizeAgentRunOrigin(override)
		if n == "" {
			return "", fmt.Errorf("CURSOR_AGENT_URL is not a valid https *.cursor.sh agent origin")
		}
		return n, nil
	}
	key := cacheKey(accessToken, APIBaseURL)
	resolveM.Lock()
	if v, ok := resolved[key]; ok {
		resolveM.Unlock()
		return v, nil
	}
	ch, pending := inflight[key]
	if pending {
		resolveM.Unlock()
		<-ch
		resolveM.Lock()
		v := resolved[key]
		resolveM.Unlock()
		if v == "" {
			return "", fmt.Errorf("cursor_get_server_config_failed")
		}
		return v, nil
	}
	ch = make(chan struct{})
	inflight[key] = ch
	resolveM.Unlock()
	url, err := fetchAgentURL(ctx, accessToken, kind)
	resolveM.Lock()
	delete(inflight, key)
	if err == nil {
		resolved[key] = url
	}
	close(ch)
	resolveM.Unlock()
	return url, err
}

func fetchAgentURL(ctx context.Context, accessToken string, kind ClientKind) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	payload, _ := json.Marshal(map[string]any{"telem_enabled": false})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, APIBaseURL+ServerConfigPath, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("connect-protocol-version", "1")
	for k, v := range ClientHeaders(kind) {
		req.Header.Set(k, v)
	}
	res, err := httpx.Client().Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("cursor_get_server_config_failed:%d", res.StatusCode)
	}
	raw, _ := io.ReadAll(res.Body)
	var body map[string]any
	if json.Unmarshal(raw, &body) != nil {
		return "", fmt.Errorf("cursor_get_server_config_malformed_json")
	}
	cfg, _ := body["agentUrlConfig"].(map[string]any)
	rawURL := ""
	if cfg != nil {
		rawURL, _ = cfg["agentnUrl"].(string)
		if rawURL == "" {
			rawURL, _ = cfg["agentUrl"].(string)
		}
	}
	normalized := NormalizeAgentRunOrigin(rawURL)
	if normalized == "" {
		if rawURL != "" {
			return "", fmt.Errorf("cursor_get_server_config_invalid_agent_url")
		}
		return "", fmt.Errorf("cursor_get_server_config_missing_agent_url")
	}
	return normalized, nil
}

func cacheKey(token, apiBase string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])[:16] + "|" + apiBase
}

func ResetAgentURLCache() {
	resolveM.Lock()
	defer resolveM.Unlock()
	resolved = map[string]string{}
	inflight = map[string]chan struct{}{}
}
