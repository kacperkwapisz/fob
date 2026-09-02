package sub

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"time"

	"github.com/kacperkwapisz/fob/internal/domain"
	"github.com/kacperkwapisz/fob/internal/provider"
	"github.com/kacperkwapisz/fob/internal/store"
	"github.com/kacperkwapisz/fob/internal/translate"
)

const (
	HandlerTimeout    = 12 * time.Second
	CredentialTimeout = 8 * time.Second
	refreshSkewMS     = 5 * 60 * 1000
)

type Window struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	UsedPercent *float64 `json:"used_percent,omitempty"`
	ResetsAt    *int64   `json:"resets_at,omitempty"`
	Detail      string   `json:"detail,omitempty"`
}

type Snapshot struct {
	ID       string            `json:"id"`
	Provider domain.ProviderID `json:"provider"`
	Label    string            `json:"label"`
	OK       bool              `json:"ok"`
	Error    string            `json:"error,omitempty"`
	Plan     string            `json:"plan,omitempty"`
	Note     string            `json:"note,omitempty"`
	Windows  []Window          `json:"windows,omitempty"`
}

type Fetcher func(ctx context.Context, credential domain.Credential) (Snapshot, error)

var fetchers = map[domain.ProviderID]Fetcher{}

func Register(id domain.ProviderID, fn Fetcher) {
	fetchers[id] = fn
}

func Collect(ctx context.Context, vault *store.Vault, executors map[domain.ProviderID]provider.Executor) []Snapshot {
	if vault == nil {
		return []Snapshot{}
	}
	creds, err := vault.List()
	if err != nil || len(creds) == 0 {
		return []Snapshot{}
	}
	ctx, cancel := context.WithTimeout(ctx, HandlerTimeout)
	defer cancel()

	out := make([]Snapshot, len(creds))
	var wg sync.WaitGroup
	for i, cred := range creds {
		wg.Add(1)
		go func(i int, cred domain.Credential) {
			defer wg.Done()
			out[i] = fetchOne(ctx, vault, executors, cred)
		}(i, cred)
	}
	wg.Wait()
	var kept []Snapshot
	for _, s := range out {
		if s.ID == "" {
			continue
		}
		kept = append(kept, s)
	}
	if kept == nil {
		kept = []Snapshot{}
	}
	return kept
}

func fetchOne(ctx context.Context, vault *store.Vault, executors map[domain.ProviderID]provider.Executor, cred domain.Credential) Snapshot {
	fn := fetchers[cred.Provider]
	if fn == nil {
		return Snapshot{}
	}
	base := Snapshot{ID: cred.ID, Provider: cred.Provider, Label: cred.Label}
	ctx, cancel := context.WithTimeout(ctx, CredentialTimeout)
	defer cancel()
	if nearExpiry(cred) {
		if ex := executors[cred.Provider]; ex != nil {
			if refreshed, err := ex.Refresh(ctx, cred); err == nil {
				cred = refreshed
				_, _ = vault.Save(store.SaveCredential{
					ID: cred.ID, Provider: cred.Provider, Label: cred.Label, Tokens: cred.Tokens, ExpiresAt: cred.ExpiresAt,
				})
			}
		}
	}
	got, err := fn(ctx, cred)
	if err != nil {
		base.Error = err.Error()
		return base
	}
	got.ID = cred.ID
	got.Provider = cred.Provider
	if got.Label == "" {
		got.Label = cred.Label
	}
	got.OK = true
	return got
}

func nearExpiry(c domain.Credential) bool {
	return c.ExpiresAt != nil && *c.ExpiresAt-time.Now().UnixMilli() < refreshSkewMS
}

func AsMap(v any) map[string]any { return translate.AsMap(v) }

func AsStr(v any, fallback ...string) string { return translate.AsStr(v, fallback...) }

func Num(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func Ptr(n float64) *float64 { return &n }

func ClampPercent(n float64) float64 {
	if n < 0 {
		return 0
	}
	if n > 100 {
		return 100
	}
	return n
}

func UnixMaybe(v any) *int64 {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		if t == "" {
			return nil
		}
		f, err := strconv.ParseFloat(t, 64)
		if err == nil && f > 0 {
			return unixPtr(f)
		}
		if parsed, err := time.Parse(time.RFC3339, t); err == nil {
			ms := parsed.UnixMilli()
			return &ms
		}
		return nil
	default:
		n, ok := Num(t)
		if !ok || n <= 0 {
			return nil
		}
		return unixPtr(n)
	}
}

func unixPtr(n float64) *int64 {
	ms := int64(n)
	if n < 1e12 {
		ms = int64(n * 1000)
	}
	return &ms
}

func FirstMap(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			return v
		}
	}
	return nil
}
