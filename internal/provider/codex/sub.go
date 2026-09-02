package codex

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kacperkwapisz/fob/internal/domain"
	"github.com/kacperkwapisz/fob/internal/provider"
	"github.com/kacperkwapisz/fob/internal/sub"
	"github.com/kacperkwapisz/fob/internal/translate"
)

const usageURL = "https://chatgpt.com/backend-api/wham/usage"

const (
	fiveHourSeconds = 18000
	weekSeconds     = 604800
	minMonthSeconds = 28 * 24 * 60 * 60
	maxMonthSeconds = 31 * 24 * 60 * 60
)

func init() { sub.Register(domain.ProviderCodex, FetchSub) }

func FetchSub(ctx context.Context, credential domain.Credential) (sub.Snapshot, error) {
	headers := map[string]string{
		"Authorization": "Bearer " + credential.Tokens.AccessToken,
		"Accept":        "application/json",
		"Originator":    Originator,
		"User-Agent":    UserAgent,
	}
	if id := accountID(credential); id != "" {
		headers["ChatGPT-Account-Id"] = id
	}
	res, err := provider.GetJSON(ctx, usageURL, headers)
	if err != nil {
		return sub.Snapshot{}, err
	}
	body, _, err := provider.ReadJSON(res)
	if err != nil {
		return sub.Snapshot{}, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return sub.Snapshot{}, fmt.Errorf("codex sub %d", res.StatusCode)
	}
	return ParseSub(body), nil
}

func ParseSub(raw any) sub.Snapshot {
	u := sub.AsMap(raw)
	snap := sub.Snapshot{Plan: titlePlan(sub.AsStr(sub.FirstMap(u, "plan_type", "planType")))}
	credits := sub.AsMap(sub.FirstMap(u, "rate_limit_reset_credits", "rateLimitResetCredits"))
	if n, ok := sub.Num(sub.FirstMap(credits, "available_count", "availableCount")); ok {
		snap.Note = fmt.Sprintf("%.0f reset credits", n)
	}
	rate := sub.AsMap(sub.FirstMap(u, "rate_limit", "rateLimit"))
	review := sub.AsMap(sub.FirstMap(u, "code_review_rate_limit", "codeReviewRateLimit"))
	snap.Windows = append(snap.Windows, classifyWindows(rate, "five-hour", "5 hours", "weekly", "7 days", "monthly", "Monthly")...)
	snap.Windows = append(snap.Windows, classifyWindows(review, "code-review-five-hour", "Code review 5 hours", "code-review-weekly", "Code review 7 days", "code-review-monthly", "Code review monthly")...)
	additional := translate.AsArr(sub.FirstMap(u, "additional_rate_limits", "additionalRateLimits"))
	for i, item := range additional {
		m := sub.AsMap(item)
		name := sub.AsStr(sub.FirstMap(m, "limit_name", "limitName", "metered_feature", "meteredFeature"))
		if name == "" {
			name = fmt.Sprintf("extra %d", i+1)
		}
		info := sub.AsMap(sub.FirstMap(m, "rate_limit", "rateLimit"))
		id := slug(name)
		snap.Windows = append(snap.Windows, classifyWindows(info, id+"-five-hour", name+" 5 hours", id+"-weekly", name+" 7 days", id+"-monthly", name+" monthly")...)
	}
	return snap
}

func classifyWindows(limit map[string]any, fiveID, fiveLabel, weekID, weekLabel, monthID, monthLabel string) []sub.Window {
	if len(limit) == 0 {
		return nil
	}
	primary := sub.AsMap(sub.FirstMap(limit, "primary_window", "primaryWindow"))
	secondary := sub.AsMap(sub.FirstMap(limit, "secondary_window", "secondaryWindow"))
	reached := limit["limit_reached"] == true || limit["limitReached"] == true || limit["allowed"] == false
	five, week := pickWindows(primary, secondary)
	var out []sub.Window
	if w := windowOf(five, fiveID, fiveLabel, reached); w != nil {
		out = append(out, *w)
	}
	if week != nil {
		id, label := weekID, weekLabel
		if monthly(week) {
			id, label = monthID, monthLabel
		}
		if w := windowOf(week, id, label, reached); w != nil {
			out = append(out, *w)
		}
	}
	return out
}

func pickWindows(primary, secondary map[string]any) (five, week map[string]any) {
	cands := []map[string]any{primary, secondary}
	for _, w := range cands {
		if len(w) == 0 {
			continue
		}
		sec := windowSeconds(w)
		if sec == fiveHourSeconds && five == nil {
			five = w
		} else if (sec == weekSeconds || monthly(w)) && week == nil {
			week = w
		}
	}
	if five == nil && len(primary) > 0 && !sameWindow(primary, week) {
		five = primary
	}
	if week == nil && len(secondary) > 0 && !sameWindow(secondary, five) {
		week = secondary
	}
	return five, week
}

func windowOf(w map[string]any, id, label string, reached bool) *sub.Window {
	if len(w) == 0 {
		return nil
	}
	used, ok := sub.Num(sub.FirstMap(w, "used_percent", "usedPercent"))
	reset := sub.UnixMaybe(sub.FirstMap(w, "reset_at", "resetAt"))
	if reset == nil {
		if n, ok := sub.Num(sub.FirstMap(w, "reset_after_seconds", "resetAfterSeconds")); ok && n >= 0 {
			ms := nowMs() + int64(n*1000)
			reset = &ms
		}
	}
	if !ok && reached {
		used, ok = 100, true
	}
	if !ok && reset == nil {
		return nil
	}
	out := &sub.Window{ID: id, Label: label, ResetsAt: reset}
	if ok {
		used = sub.ClampPercent(used)
		out.UsedPercent = sub.Ptr(used)
	}
	return out
}

func windowSeconds(w map[string]any) float64 {
	n, _ := sub.Num(sub.FirstMap(w, "limit_window_seconds", "limitWindowSeconds"))
	return n
}

func monthly(w map[string]any) bool {
	s := windowSeconds(w)
	return s >= minMonthSeconds && s <= maxMonthSeconds
}

func sameWindow(a, b map[string]any) bool {
	return len(a) > 0 && len(b) > 0 && fmt.Sprint(a) == fmt.Sprint(b)
}

func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	dash := false
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "extra"
	}
	return out
}

func titlePlan(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "pro":
		return "Pro"
	case "plus":
		return "Plus"
	case "team":
		return "Team"
	case "free":
		return "Free"
	case "prolite", "go":
		return "Pro"
	default:
		if s == "" {
			return ""
		}
		return strings.ToUpper(s[:1]) + s[1:]
	}
}

var nowMs = func() int64 { return time.Now().UnixMilli() }
