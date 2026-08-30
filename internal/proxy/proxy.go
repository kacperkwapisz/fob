package proxy

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/kacperkwapisz/fob/internal/domain"
	"github.com/kacperkwapisz/fob/internal/env"
	"github.com/kacperkwapisz/fob/internal/httpx"
	"github.com/kacperkwapisz/fob/internal/provider"
	"github.com/kacperkwapisz/fob/internal/provider/cursor"
	"github.com/kacperkwapisz/fob/internal/store"
	"github.com/kacperkwapisz/fob/internal/translate"
)

const (
	SettingCursorPrefix       = "cursor.prefix"
	SettingCursorGrokFailover = "cursor.grokFailover"
)

type Fob struct {
	Env       *env.Env
	Vault     *store.Vault
	Keys      *store.KeyStore
	Usage     *store.UsageStore
	Prices    *store.PriceStore
	Executors map[domain.ProviderID]provider.Executor
	Settings  *store.SettingsStore
}

type Request struct {
	Inbound        domain.InboundFormat
	Body           any
	Key            domain.LocalKey
	Stream         bool
	CountTokens    bool
	Compact        bool
	StickyID       string
	InboundHeaders map[string]string
}

type Result struct {
	OK     bool
	Status int
	Body   any
	Stream <-chan string
}

var sticky sync.Map

func Proxy(ctx context.Context, fob *Fob, req Request) (Result, error) {
	started := time.Now().UnixMilli()
	model := requestedModel(req.Body)
	if model == "" {
		return Result{OK: false, Status: 400, Body: httpx.OpenAIError("invalid_request_error", "model is required")}, nil
	}
	chain := resolveProviderChain(fob, model)
	if len(chain) == 0 {
		return Result{OK: false, Status: 404, Body: httpx.OpenAIError("invalid_request_error", "unknown model: "+model)}, nil
	}
	if req.Key.DailyCap != nil {
		used, err := fob.Usage.TodayTokensForKey(req.Key.ID)
		if err != nil {
			return Result{}, err
		}
		if used >= *req.Key.DailyCap {
			return Result{OK: false, Status: 429, Body: httpx.OpenAIError("rate_limit_exceeded", "daily token cap reached")}, nil
		}
	}

	var lastStatus int
	var lastBody any
	attempted := false
	firstByte := false

	for hopIndex, hop := range chain {
		if err := fob.Keys.Allows(req.Key, hop.Provider, hop.Model); err != nil {
			lastStatus, lastBody = 403, httpx.OpenAIError("permission_denied", err.Error())
			continue
		}
		executor := fob.Executors[hop.Provider]
		if executor == nil {
			continue
		}
		inboundBody := req.Body
		if req.Inbound == domain.InboundOpenAIResponses && hop.Provider != domain.ProviderCodex {
			inboundBody = translate.FlattenCodexMultiAgent(req.Body)
		}
		translated := translate.TranslateRequest(req.Inbound, executor.Format(), hop.Model, req.Stream, inboundBody)
		creds, err := pickCredentials(fob, hop.Provider, req.Key.ID, req.StickyID)
		if err != nil {
			return Result{}, err
		}
		if len(creds) == 0 {
			lastStatus, lastBody = 503, httpx.OpenAIError("server_error", "no "+string(hop.Provider)+" credential is connected")
			continue
		}
		attempted = true
		for i, cred := range creds {
			if nearExpiry(cred) {
				if refreshed, err := executor.Refresh(ctx, cred); err == nil {
					cred = refreshed
					_, _ = fob.Vault.Save(store.SaveCredential{
						ID: cred.ID, Provider: cred.Provider, Label: cred.Label, Tokens: cred.Tokens, ExpiresAt: cred.ExpiresAt,
					})
				}
			}
			result, err := executor.Execute(ctx, cred, translated.Body, provider.ExecuteOptions{
				Stream: req.Stream, CountTokens: req.CountTokens, Compact: req.Compact,
				InboundHeaders: req.InboundHeaders, CallerKey: req.Key.ID,
			})
			if err != nil {
				return Result{}, err
			}
			if !result.OK && result.Status == 401 {
				if refreshed, err := executor.Refresh(ctx, cred); err == nil {
					cred = refreshed
					_, _ = fob.Vault.Save(store.SaveCredential{
						ID: cred.ID, Provider: cred.Provider, Label: cred.Label, Tokens: cred.Tokens, ExpiresAt: cred.ExpiresAt,
					})
					result, err = executor.Execute(ctx, cred, translated.Body, provider.ExecuteOptions{
						Stream: req.Stream, CountTokens: req.CountTokens, Compact: req.Compact,
						InboundHeaders: req.InboundHeaders, CallerKey: req.Key.ID,
					})
					if err != nil {
						return Result{}, err
					}
				}
			}
			if !result.OK {
				lastStatus, lastBody = result.Status, result.Body
				moreCreds := result.Retryable && i < len(creds)-1 && !firstByte
				moreHops := result.Retryable && hopIndex < len(chain)-1 && !firstByte
				if moreCreds {
					continue
				}
				if moreHops {
					break
				}
				record(fob, req, hop.Provider, hop.Model, started, "error", 0, 0, 0, 0)
				return Result{OK: false, Status: result.Status, Body: result.Body}, nil
			}
			sticky.Store(req.Key.ID, cred.ID)
			if req.Stream && result.Stream != nil {
				out := make(chan string)
				go func() {
					defer close(out)
					state := translate.EmptyStreamState()
					sentDone := false
					for chunk := range result.Stream {
						firstByte = true
						lines := translate.TranslateStream(req.Inbound, executor.Format(), hop.Model, req.Body, chunk, &state)
						for _, line := range lines {
							if line == "data: [DONE]" {
								sentDone = true
							}
							if !strings.HasSuffix(line, "\n") {
								line += "\n\n"
							}
							out <- line
						}
					}
					if done := translate.DoneLine(req.Inbound); done != "" && !sentDone {
						out <- done + "\n\n"
					}
					status := "ok"
					if !state.Finished {
						status = "error"
					}
					record(fob, req, hop.Provider, hop.Model, started, status, state.PromptTokens, state.CompletionTokens, state.CacheRead, state.CacheWrite)
				}()
				return Result{OK: true, Status: 200, Stream: out}, nil
			}
			body := translate.TranslateResponse(req.Inbound, executor.Format(), hop.Model, req.Body, result.Body)
			pt, ct, cr, cw := usageFrom(body, result.Body)
			record(fob, req, hop.Provider, hop.Model, started, "ok", pt, ct, cr, cw)
			return Result{OK: true, Status: 200, Body: body}, nil
		}
	}
	record(fob, req, chain[0].Provider, chain[0].Model, started, "error", 0, 0, 0, 0)
	status := lastStatus
	if status == 0 {
		if attempted {
			status = 502
		} else {
			status = 503
		}
	}
	body := lastBody
	if body == nil {
		body = httpx.OpenAIError("server_error", "all credentials failed")
	}
	return Result{OK: false, Status: status, Body: body}, nil
}

func ListModels(fob *Fob) []domain.ModelInfo {
	creds, _ := fob.Vault.List()
	seen := map[domain.ProviderID]bool{}
	var order []domain.ProviderID
	for _, c := range creds {
		if seen[c.Provider] {
			continue
		}
		seen[c.Provider] = true
		order = append(order, c.Provider)
	}
	prefix := false
	if fob.Settings != nil {
		v, _ := fob.Settings.Get(SettingCursorPrefix)
		prefix = v == "1"
	}
	var models []domain.ModelInfo
	for _, providerID := range order {
		ex := fob.Executors[providerID]
		if ex == nil {
			continue
		}
		if providerID == domain.ProviderCursor {
			models = append(models, cursorModelsForList(ex, prefix)...)
			continue
		}
		models = append(models, ex.Models()...)
	}
	if models == nil {
		models = []domain.ModelInfo{}
	}
	return models
}

func cursorModelsForList(ex provider.Executor, prefix bool) []domain.ModelInfo {
	models := ex.Models()
	if !prefix {
		return models
	}
	out := make([]domain.ModelInfo, len(models))
	for i, m := range models {
		if !strings.HasPrefix(m.ID, "cursor/") {
			m.ID = "cursor/" + m.ID
		}
		out[i] = m
	}
	return out
}

type hop struct {
	Provider domain.ProviderID
	Model    string
}

func pickCredentials(fob *Fob, providerID domain.ProviderID, keyID, stickyID string) ([]domain.Credential, error) {
	all, err := fob.Vault.List(providerID)
	if err != nil {
		return nil, err
	}
	if providerID == domain.ProviderCursor {
		oauth := []domain.Credential{}
		keys := []domain.Credential{}
		for _, c := range all {
			if kind, _ := c.Tokens.Extra["kind"].(string); kind == "api_key" {
				keys = append(keys, c)
			} else {
				oauth = append(oauth, c)
			}
		}
		all = append(oauth, keys...)
	}
	if len(all) == 0 {
		return nil, nil
	}
	preferred := stickyID
	if preferred == "" {
		if v, ok := sticky.Load(keyID); ok {
			preferred, _ = v.(string)
		}
	}
	if preferred == "" {
		return all, nil
	}
	var hit *domain.Credential
	rest := []domain.Credential{}
	for _, c := range all {
		if c.ID == preferred {
			cp := c
			hit = &cp
		} else {
			rest = append(rest, c)
		}
	}
	if hit == nil {
		return all, nil
	}
	return append([]domain.Credential{*hit}, rest...), nil
}

func resolveProviderChain(fob *Fob, rawModel string) []hop {
	forced := strings.HasPrefix(rawModel, "cursor/")
	model := cursor.StripPublicPrefix(rawModel)
	known := cursor.KnownIDs()
	restored := cursor.RestoreWirePrefix(model, known)
	cursorOK := cursorConnected(fob)
	if forced || strings.HasPrefix(restored, "composer-") || strings.HasPrefix(restored, "cursor-") || strings.HasPrefix(restored, "gemini-") || strings.HasPrefix(restored, "kimi-") || strings.HasPrefix(restored, "glm-") || restored == "cursor-auto" {
		var hops []hop
		if cursorOK {
			hops = append(hops, hop{domain.ProviderCursor, restored})
		}
		if len(hops) > 0 && grokFailoverOn(fob) && strings.Contains(restored, "grok") {
			if p, native, ok := cursor.MapWireToNative(restored); ok {
				list, _ := fob.Vault.List(p)
				if len(list) > 0 {
					hops = append(hops, hop{p, native})
				}
			}
		}
		return hops
	}
	if strings.HasPrefix(model, "claude-") {
		hops := []hop{{domain.ProviderClaude, model}}
		if cursorOK {
			if mapped := cursor.MapNativeToWire(model, known); mapped != "" {
				hops = append(hops, hop{domain.ProviderCursor, mapped})
			}
		}
		return hops
	}
	if strings.HasPrefix(model, "gpt-") || strings.HasPrefix(model, "o1") || strings.HasPrefix(model, "o3") || strings.HasPrefix(model, "o4") {
		hops := []hop{{domain.ProviderCodex, model}}
		if cursorOK {
			if mapped := cursor.MapNativeToWire(model, known); mapped != "" {
				hops = append(hops, hop{domain.ProviderCursor, mapped})
			}
		}
		return hops
	}
	if strings.HasPrefix(model, "grok-") {
		hops := []hop{{domain.ProviderGrok, model}}
		if grokFailoverOn(fob) && cursorOK {
			if mapped := cursor.MapNativeToWire(model, known); mapped != "" {
				hops = append(hops, hop{domain.ProviderCursor, mapped})
			}
		}
		return hops
	}
	if domain.IsProviderID(model) {
		return []hop{{domain.ProviderID(model), model}}
	}
	creds, _ := fob.Vault.List()
	for _, c := range creds {
		ex := fob.Executors[c.Provider]
		if ex == nil {
			continue
		}
		for _, m := range ex.Models() {
			if m.ID == model {
				return []hop{{c.Provider, model}}
			}
		}
	}
	return nil
}

func cursorConnected(fob *Fob) bool {
	list, _ := fob.Vault.List(domain.ProviderCursor)
	return len(list) > 0
}

func grokFailoverOn(fob *Fob) bool {
	if fob.Settings == nil {
		return false
	}
	v, _ := fob.Settings.Get(SettingCursorGrokFailover)
	return v == "1"
}

func requestedModel(body any) string {
	m, _ := body.(map[string]any)
	s, _ := m["model"].(string)
	return s
}

func nearExpiry(c domain.Credential) bool {
	return c.ExpiresAt != nil && *c.ExpiresAt-time.Now().UnixMilli() < 5*60*1000
}

func usageFrom(translated, upstream any) (pt, ct, cr, cw int64) {
	t := asMap(translated)
	u := asMap(upstream)
	usage := asMap(t["usage"])
	if len(usage) == 0 {
		usage = asMap(u["usage"])
	}
	details := asMap(usage["prompt_tokens_details"])
	if len(details) == 0 {
		details = asMap(usage["input_tokens_details"])
	}
	pt = int64From(usage["prompt_tokens"], usage["input_tokens"])
	ct = int64From(usage["completion_tokens"], usage["output_tokens"])
	cr = int64From(details["cached_tokens"], usage["cache_read_input_tokens"])
	cw = int64From(details["cache_write_tokens"], usage["cache_creation_input_tokens"])
	return
}

func record(fob *Fob, req Request, providerID domain.ProviderID, model string, started int64, status string, pt, ct, cr, cw int64) {
	priceProvider := map[domain.ProviderID]string{
		domain.ProviderClaude: "anthropic",
		domain.ProviderCodex:  "openai",
		domain.ProviderGrok:   "xai",
		domain.ProviderCursor: "cursor",
	}[providerID]
	usd := fob.Prices.Estimate(priceProvider, model, pt, ct, cr, cw)
	_ = fob.Usage.Record(domain.UsageEvent{
		TS: time.Now().UnixMilli(), KeyID: req.Key.ID, Provider: providerID, Model: model, Inbound: req.Inbound,
		PromptTokens: pt, CompletionTokens: ct, CacheReadTokens: cr, CacheWriteTokens: cw,
		LatencyMs: time.Now().UnixMilli() - started, Status: status, USD: usd,
	})
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}

func int64From(vals ...any) int64 {
	for _, v := range vals {
		switch n := v.(type) {
		case float64:
			return int64(n)
		case int64:
			return n
		case int:
			return int64(n)
		}
	}
	return 0
}
