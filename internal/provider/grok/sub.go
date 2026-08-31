package grok

import (
	"context"
	"fmt"
	"sync"

	"github.com/kacperkwapisz/fob/internal/domain"
	"github.com/kacperkwapisz/fob/internal/provider"
	"github.com/kacperkwapisz/fob/internal/sub"
	"github.com/kacperkwapisz/fob/internal/translate"
)

const (
	weeklyURL  = api + "/billing?format=credits"
	monthlyURL = api + "/billing"
	superGrok  = 15_000
	superHeavy = 150_000
)

func init() { sub.Register(domain.ProviderGrok, FetchSub) }

func FetchSub(ctx context.Context, credential domain.Credential) (sub.Snapshot, error) {
	headers := map[string]string{
		"Authorization":            "Bearer " + credential.Tokens.AccessToken,
		"x-xai-token-auth":         "xai-grok-cli",
		"x-grok-client-version":    cliVersion,
		"Accept":                   "*/*",
		"User-Agent":               "xai-grok-workspace/" + cliVersion,
		"x-grok-client-identifier": "grok-shell",
	}
	var weekly, monthly any
	var weeklyErr, monthlyErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		weekly, weeklyErr = getJSON(ctx, weeklyURL, headers)
	}()
	go func() {
		defer wg.Done()
		monthly, monthlyErr = getJSON(ctx, monthlyURL, headers)
	}()
	wg.Wait()
	if weekly == nil && monthly == nil {
		if weeklyErr != nil {
			return sub.Snapshot{}, weeklyErr
		}
		if monthlyErr != nil {
			return sub.Snapshot{}, monthlyErr
		}
		return sub.Snapshot{}, fmt.Errorf("grok sub empty")
	}
	return ParseSub(weekly, monthly), nil
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
		return nil, fmt.Errorf("grok sub %d", res.StatusCode)
	}
	return body, nil
}

func ParseSub(weekly, monthly any) sub.Snapshot {
	wCfg := billingConfig(weekly)
	mCfg := billingConfig(monthly)
	snap := sub.Snapshot{}
	limit := cents(sub.FirstMap(mCfg, "monthlyLimit", "monthly_limit"))
	if limit == 0 {
		limit = cents(sub.FirstMap(wCfg, "monthlyLimit", "monthly_limit"))
	}
	snap.Plan = grokPlan(limit)
	if w := weeklyWindow(wCfg); w != nil {
		snap.Windows = append(snap.Windows, *w)
	}
	for _, p := range translate.AsArr(sub.FirstMap(wCfg, "productUsage", "product_usage")) {
		m := sub.AsMap(p)
		name := sub.AsStr(m["product"])
		if name == "" {
			continue
		}
		used, ok := sub.Num(sub.FirstMap(m, "usagePercent", "usage_percent"))
		if !ok {
			continue
		}
		used = sub.ClampPercent(used)
		snap.Windows = append(snap.Windows, sub.Window{ID: "product-" + name, Label: name, UsedPercent: sub.Ptr(used)})
	}
	if w := monthlyWindow(mCfg, wCfg); w != nil {
		snap.Windows = append(snap.Windows, *w)
	}
	return snap
}

func billingConfig(raw any) map[string]any {
	return sub.AsMap(sub.AsMap(raw)["config"])
}

func weeklyWindow(cfg map[string]any) *sub.Window {
	if len(cfg) == 0 {
		return nil
	}
	period := sub.AsMap(sub.FirstMap(cfg, "currentPeriod", "current_period"))
	used, ok := sub.Num(sub.FirstMap(cfg, "creditUsagePercent", "credit_usage_percent", "usagePercent", "usage_percent"))
	end := sub.UnixMaybe(sub.FirstMap(period, "end"))
	if end == nil {
		end = sub.UnixMaybe(sub.FirstMap(cfg, "periodEnd", "period_end"))
	}
	if !ok && end == nil {
		return nil
	}
	w := &sub.Window{ID: "weekly", Label: "Weekly", ResetsAt: end}
	if ok {
		used = sub.ClampPercent(used)
		w.UsedPercent = sub.Ptr(used)
	}
	return w
}

func monthlyWindow(monthly, weekly map[string]any) *sub.Window {
	cfg := monthly
	if len(cfg) == 0 {
		cfg = weekly
	}
	limit := cents(sub.FirstMap(cfg, "monthlyLimit", "monthly_limit"))
	used := cents(sub.FirstMap(cfg, "used"))
	if used == 0 {
		used = cents(sub.FirstMap(cfg, "includedUsed", "included_used"))
	}
	end := sub.UnixMaybe(sub.FirstMap(cfg, "billingPeriodEnd", "billing_period_end"))
	if end == nil {
		end = sub.UnixMaybe(sub.AsMap(sub.FirstMap(cfg, "currentPeriod", "current_period"))["end"])
	}
	if limit == 0 && used == 0 && end == nil {
		return nil
	}
	w := &sub.Window{ID: "monthly", Label: "Monthly credits", ResetsAt: end, Detail: moneyPair(limit-used, limit)}
	if limit > 0 {
		pct := sub.ClampPercent(float64(used) / float64(limit) * 100)
		w.UsedPercent = sub.Ptr(pct)
	}
	return w
}

func grokPlan(limitCents int64) string {
	switch limitCents {
	case superGrok:
		return "SuperGrok"
	case superHeavy:
		return "SuperGrok Heavy"
	default:
		return ""
	}
}

func cents(v any) int64 {
	if v == nil {
		return 0
	}
	if m := sub.AsMap(v); len(m) > 0 {
		if n, ok := sub.Num(m["val"]); ok {
			return int64(n)
		}
	}
	n, _ := sub.Num(v)
	return int64(n)
}

func moneyPair(remain, limit int64) string {
	if limit <= 0 {
		return ""
	}
	if remain < 0 {
		remain = 0
	}
	return fmt.Sprintf("$%.2f / $%.2f", float64(remain)/100, float64(limit)/100)
}
