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
