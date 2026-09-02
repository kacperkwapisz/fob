package cursor

import (
	"context"
	"fmt"
	"sync"

	"github.com/kacperkwapisz/fob/internal/domain"
	"github.com/kacperkwapisz/fob/internal/provider"
	"github.com/kacperkwapisz/fob/internal/sub"
)

const (
	periodUsagePath = "/aiserver.v1.DashboardService/GetCurrentPeriodUsage"
	planInfoPath    = "/aiserver.v1.DashboardService/GetPlanInfo"
)

func init() { sub.Register(domain.ProviderCursor, FetchSub) }

func FetchSub(ctx context.Context, credential domain.Credential) (sub.Snapshot, error) {
	access, err := accessToken(ctx, credential, ClientCLI)
	if err != nil {
		return sub.Snapshot{}, err
	}
	headers := map[string]string{
		"Authorization": "Bearer " + access,
		"Accept":        "application/json",
	}
	for k, v := range ClientHeaders(ClientCLI) {
		headers[k] = v
	}
	var usage, plan any
	var usageErr, planErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		usage, usageErr = postJSON(ctx, APIBaseURL+periodUsagePath, map[string]any{}, headers)
	}()
	go func() {
		defer wg.Done()
		plan, planErr = postJSON(ctx, APIBaseURL+planInfoPath, map[string]any{}, headers)
	}()
	wg.Wait()
	if usageErr != nil {
		return sub.Snapshot{}, usageErr
	}
	_ = planErr
	return ParseSub(usage, plan), nil
}

func postJSON(ctx context.Context, rawURL string, body any, headers map[string]string) (any, error) {
	res, err := provider.PostJSON(ctx, rawURL, body, headers)
	if err != nil {
		return nil, err
	}
	parsed, _, err := provider.ReadJSON(res)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("cursor sub %d", res.StatusCode)
	}
	return parsed, nil
}

func ParseSub(usage, plan any) sub.Snapshot {
	u := sub.AsMap(usage)
	p := sub.AsMap(sub.AsMap(plan)["planInfo"])
	if len(p) == 0 {
		p = sub.AsMap(sub.AsMap(plan)["plan_info"])
	}
	snap := sub.Snapshot{Plan: sub.AsStr(p["planName"], sub.AsStr(p["plan_name"]))}
	if price := sub.AsStr(p["price"]); price != "" && snap.Plan != "" {
		snap.Plan = snap.Plan + " · " + price
	}
	if u["enabled"] == false {
		snap.Note = sub.AsStr(u["displayMessage"], "not shown")
		return snap
	}
	cycleEnd := sub.UnixMaybe(sub.FirstMap(u, "billingCycleEnd", "billing_cycle_end"))
	if cycleEnd == nil {
		cycleEnd = sub.UnixMaybe(sub.FirstMap(p, "billingCycleEnd", "billing_cycle_end"))
	}
	planUsage := sub.AsMap(sub.FirstMap(u, "planUsage", "plan_usage"))
	if auto, ok := sub.Num(sub.FirstMap(planUsage, "autoPercentUsed", "auto_percent_used")); ok {
		snap.Windows = append(snap.Windows, sub.Window{
			ID: "auto", Label: "Cursor Models", UsedPercent: sub.Ptr(sub.ClampPercent(auto)), ResetsAt: cycleEnd,
		})
	}
	if api, ok := sub.Num(sub.FirstMap(planUsage, "apiPercentUsed", "api_percent_used")); ok {
		snap.Windows = append(snap.Windows, sub.Window{
			ID: "api", Label: "Other Models", UsedPercent: sub.Ptr(sub.ClampPercent(api)), ResetsAt: cycleEnd,
		})
	}
	if len(snap.Windows) == 0 {
		if total, ok := sub.Num(sub.FirstMap(planUsage, "totalPercentUsed", "total_percent_used")); ok {
			snap.Windows = append(snap.Windows, sub.Window{
				ID: "cycle", Label: "This cycle", UsedPercent: sub.Ptr(sub.ClampPercent(total)), ResetsAt: cycleEnd,
			})
		}
	}
	return snap
}
