package store

import (
	"testing"
	"time"

	"github.com/kacperkwapisz/fob/internal/db"
	"github.com/kacperkwapisz/fob/internal/domain"
)

func TestUsageAndPrices(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	prices, err := NewPriceStore(d)
	if err != nil {
		t.Fatal(err)
	}
	usage := NewUsageStore(d)
	usd := prices.Estimate("anthropic", "claude-opus-4-7", 1_000_000, 0, 0, 0)
	if usd != 5 {
		t.Fatalf("usd %v", usd)
	}
	if err := usage.Record(domain.UsageEvent{
		TS:           nowMs(),
		KeyID:        "k1",
		Provider:     domain.ProviderClaude,
		Model:        "claude-opus-4-7",
		Inbound:      domain.InboundOpenAIChat,
		PromptTokens: 1_000_000,
		LatencyMs:    12,
		Status:       "ok",
		USD:          usd,
	}); err != nil {
		t.Fatal(err)
	}
	today, err := usage.Since(24 * 60 * 60 * 1000)
	if err != nil {
		t.Fatal(err)
	}
	if today.Requests != 1 || today.PromptTokens != 1_000_000 || today.USD != 5 {
		t.Fatalf("%+v", today)
	}
}

func TestUsageDailyFillsEmptyDays(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	usage := NewUsageStore(d)
	now := time.UnixMilli(nowMs()).Local()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	noon := func(daysAgo int) int64 {
		return today.AddDate(0, 0, -daysAgo).Add(12 * time.Hour).UnixMilli()
	}
	for _, ev := range []domain.UsageEvent{
		{TS: noon(20), Provider: domain.ProviderClaude, Model: "old", Inbound: domain.InboundOpenAIChat, PromptTokens: 99, Status: "ok"},
		{TS: noon(3), Provider: domain.ProviderClaude, Model: "mid", Inbound: domain.InboundOpenAIChat, PromptTokens: 5, CompletionTokens: 1, Status: "ok"},
		{TS: now.UnixMilli(), Provider: domain.ProviderCodex, Model: "new", Inbound: domain.InboundOpenAIChat, PromptTokens: 10, CompletionTokens: 2, Status: "ok"},
	} {
		if err := usage.Record(ev); err != nil {
			t.Fatal(err)
		}
	}
	days, err := usage.Daily(14)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 14 {
		t.Fatalf("len %d", len(days))
	}
	if days[13].Start != today.UnixMilli() {
		t.Fatalf("today start %d want %d", days[13].Start, today.UnixMilli())
	}
	if days[13].PromptTokens != 10 || days[13].CompletionTokens != 2 || days[13].Requests != 1 {
		t.Fatalf("today %+v", days[13])
	}
	if days[10].PromptTokens != 5 || days[10].CompletionTokens != 1 {
		t.Fatalf("3d ago %+v", days[10])
	}
	var prompt int64
	for _, day := range days {
		prompt += day.PromptTokens
	}
	if prompt != 15 {
		t.Fatalf("window prompt %d", prompt)
	}
}

func TestCursorAutoHasNoListPrice(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	prices, err := NewPriceStore(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range []string{"cursor-auto", "auto", "default"} {
		if usd := prices.Estimate("cursor", model, 1_000_000, 1_000_000, 1_000_000, 0); usd != 0 {
			t.Fatalf("%s usd %v", model, usd)
		}
	}
	if prices.Estimate("cursor", "composer-2.5", 1_000_000, 0, 0, 0) != 0.5 {
		t.Fatalf("composer usd %v", prices.Estimate("cursor", "composer-2.5", 1_000_000, 0, 0, 0))
	}
}

func TestCursorOverlaySeedsExistingCatalog(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.SQL.Exec(`INSERT INTO prices(provider, model, input, output, cache_read, cache_write, fetched_at) VALUES ('anthropic', 'claude-opus-4-7', 5, 25, 0.5, 6.25, 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SQL.Exec(`INSERT INTO prices(provider, model, input, output, cache_read, cache_write, fetched_at) VALUES ('cursor', 'cursor-auto', 0.5, 2.5, 0.2, NULL, 1)`); err != nil {
		t.Fatal(err)
	}
	prices, err := NewPriceStore(d)
	if err != nil {
		t.Fatal(err)
	}
	if prices.Estimate("cursor", "composer-2.5", 1_000_000, 0, 0, 0) != 0.5 {
		t.Fatalf("overlay missing: %v", prices.Estimate("cursor", "composer-2.5", 1_000_000, 0, 0, 0))
	}
	if prices.Estimate("cursor", "cursor-auto", 1_000_000, 0, 0, 0) != 0 {
		t.Fatalf("auto leftover priced: %v", prices.Estimate("cursor", "cursor-auto", 1_000_000, 0, 0, 0))
	}
}
