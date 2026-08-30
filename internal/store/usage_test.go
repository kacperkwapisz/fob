package store

import (
	"testing"

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
