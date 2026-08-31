package claude

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/kacperkwapisz/fob/internal/domain"
	"github.com/kacperkwapisz/fob/internal/provider"
	"github.com/kacperkwapisz/fob/internal/sub"
	"github.com/kacperkwapisz/fob/internal/translate"
)

const (
	usageURL   = "https://api.anthropic.com/api/oauth/usage"
	profileURL = "https://api.anthropic.com/api/oauth/profile"
)

func init() { sub.Register(domain.ProviderClaude, FetchSub) }

func FetchSub(ctx context.Context, credential domain.Credential) (sub.Snapshot, error) {
	headers := map[string]string{
		"Authorization":  "Bearer " + credential.Tokens.AccessToken,
		"Content-Type":   "application/json",
		"anthropic-beta": oauthBeta,
		"Accept":         "application/json, text/plain, */*",
		"User-Agent":     "axios/1.15.2",
	}
	var (
		usage    any
		usageErr error
		profile  any
		wg       sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		usage, usageErr = getJSON(ctx, usageURL, headers)
	}()
	go func() {
		defer wg.Done()
		profile, _ = getJSON(ctx, profileURL, headers)
	}()
	wg.Wait()
	if usageErr != nil {
		return sub.Snapshot{}, usageErr
	}
	return ParseSub(usage, profile), nil
}

func getJSON(ctx context.Context, rawURL string, headers map[string]string) (any, error) {
	res, err := provider.GetJSON(ctx, rawURL, headers)
	if err != nil {
		return nil, err
	}
	body, _, err := provider.ReadJSON(res)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("claude sub %d", res.StatusCode)
	}
	return body, nil
}

func ParseSub(usage, profile any) sub.Snapshot {
	u := sub.AsMap(usage)
	snap := sub.Snapshot{Plan: claudePlan(profile)}
	if extra := sub.AsMap(u["extra_usage"]); extra["is_enabled"] == true {
		used, _ := sub.Num(extra["used_credits"])
		limit, _ := sub.Num(extra["monthly_limit"])
		snap.Note = fmt.Sprintf("$%.2f / $%.2f extra", used/100, limit/100)
	}
	for _, w := range claudeWindows {
		raw := u[w.key]
		m := sub.AsMap(raw)
		if len(m) == 0 {
			continue
		}
		used, ok := sub.Num(m["utilization"])
		if !ok {
			continue
		}
		used = sub.ClampPercent(used)
		snap.Windows = append(snap.Windows, sub.Window{
			ID: w.id, Label: w.label, UsedPercent: sub.Ptr(used), ResetsAt: sub.UnixMaybe(m["resets_at"]),
		})
	}
	if fable := fableWindow(u); fable != nil {
		replaced := false
		for i, w := range snap.Windows {
			if w.ID == "seven-day-fable" {
				snap.Windows[i] = *fable
				replaced = true
				break
			}
		}
		if !replaced {
			snap.Windows = append(snap.Windows, *fable)
		}
	}
	return snap
}

var claudeWindows = []struct{ key, id, label string }{
	{"five_hour", "five-hour", "5 hours"},
	{"seven_day", "seven-day", "7 days"},
	{"seven_day_oauth_apps", "seven-day-oauth-apps", "7-day OAuth apps"},
	{"seven_day_opus", "seven-day-opus", "7-day Opus"},
	{"seven_day_sonnet", "seven-day-sonnet", "7-day Sonnet"},
	{"seven_day_cowork", "seven-day-cowork", "7-day Cowork"},
	{"iguana_necktie", "seven-day-fable", "7-day Fable"},
}

func fableWindow(u map[string]any) *sub.Window {
	var pick map[string]any
	for _, raw := range translate.AsArr(u["limits"]) {
		m := sub.AsMap(raw)
		kind := strings.ToLower(sub.AsStr(m["kind"]))
		model := strings.ToLower(sub.AsStr(sub.AsMap(sub.AsMap(m["scope"])["model"])["display_name"]))
		if kind != "weekly_scoped" || (model != "fable" && model != "fable 5") {
			continue
		}
		if _, ok := sub.Num(m["percent"]); !ok {
			continue
		}
		if m["is_active"] == true || pick == nil {
			pick = m
			if m["is_active"] == true {
				break
			}
		}
	}
	if pick == nil {
		return nil
	}
	used, _ := sub.Num(pick["percent"])
	used = sub.ClampPercent(used)
	return &sub.Window{ID: "seven-day-fable", Label: "7-day Fable", UsedPercent: sub.Ptr(used), ResetsAt: sub.UnixMaybe(pick["resets_at"])}
}

func claudePlan(profile any) string {
	p := sub.AsMap(profile)
	account := sub.AsMap(p["account"])
	org := sub.AsMap(p["organization"])
	if flag(account["has_claude_max"]) {
		return "Max"
	}
	if flag(account["has_claude_pro"]) {
		return "Pro"
	}
	if strings.EqualFold(sub.AsStr(org["organization_type"]), "claude_team") && strings.EqualFold(sub.AsStr(org["subscription_status"]), "active") {
		return "Team"
	}
	if account["has_claude_max"] == false && account["has_claude_pro"] == false {
		return "Free"
	}
	return ""
}

func flag(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		s := strings.ToLower(strings.TrimSpace(t))
		return s == "true" || s == "1" || s == "yes"
	case float64:
		return t != 0
	default:
		return false
	}
}
