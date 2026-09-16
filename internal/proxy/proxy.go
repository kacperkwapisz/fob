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
	"github.com/kacperkwapisz/fob/internal/provider/openai"
	"github.com/kacperkwapisz/fob/internal/store"
	"github.com/kacperkwapisz/fob/internal/translate"
)

type liveModels interface {
	ModelsFor(context.Context, domain.Credential) []domain.ModelInfo
}

const (
	SettingCursorPrefix       = "cursor.prefix"
	SettingCursorGrokFailover = "cursor.grokFailover"
	SettingCursorListFast     = "cursor.listFast"
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
		return failOut(req, 400, httpx.KindRequest, "", "", "model is required", `set "model" in the request body`), nil
	}
	chain := resolveProviderChain(fob, model)
	if len(chain) == 0 {
		return failOut(req, 404, httpx.KindModel, "", model, "unknown model: "+model, "GET /v1/models for ids this instance can serve"), nil
	}
	if req.Key.DailyCap != nil {
		used, err := fob.Usage.TodayTokensForKey(req.Key.ID)
		if err != nil {
			return Result{}, err
		}
		if used >= *req.Key.DailyCap {
			return failOut(req, 429, httpx.KindCap, "", model, "daily token cap reached", "wait until UTC midnight, or raise the key's cap in the panel"), nil
		}
	}

	var last provider.ExecuteResult
	var lastHop hop
	attempted := false
	firstByte := false
	route := string(req.Inbound)

	for hopIndex, hop := range chain {
		if err := fob.Keys.Allows(req.Key, hop.Provider, hop.Model); err != nil {
			last = provider.ExecuteResult{OK: false, Status: 403, Message: err.Error()}
			lastHop = hop
			continue
		}
		executor := fob.Executors[hop.Provider]
		if executor == nil {
			last = provider.ExecuteResult{OK: false, Status: 503, Message: "provider " + string(hop.Provider) + " is not available"}
			lastHop = hop
			continue
		}
		inboundBody := req.Body
		if req.Inbound == domain.InboundOpenAIResponses && hop.Provider != domain.ProviderCodex {
			inboundBody = translate.FlattenCodexMultiAgent(req.Body)
		}
		translated := translate.TranslateRequest(req.Inbound, executor.Format(), hop.Model, req.Stream, inboundBody)
		creds, err := pickCredentials(fob, hop.Provider, hop.Model, req.Key.ID, req.StickyID)
		if err != nil {
			return Result{}, err
		}
		if len(creds) == 0 {
			last = provider.ExecuteResult{OK: false, Status: 503, Message: "no " + string(hop.Provider) + " credential is connected"}
			lastHop = hop
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
				} else {
					logRefresh(hop.Provider, cred.Label, route, err)
				}
			}
			result, err := executor.Execute(ctx, cred, translated.Body, provider.ExecuteOptions{
				Stream: req.Stream, CountTokens: req.CountTokens, Compact: req.Compact,
				InboundHeaders: req.InboundHeaders, CallerKey: req.Key.ID,
			})
			if err != nil {
				result = provider.FailFromErr(err)
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
						result = provider.FailFromErr(err)
					}
				} else {
					logRefresh(hop.Provider, cred.Label, route, err)
				}
			}
			if !result.OK {
				last, lastHop = result, hop
				moreCreds := result.Retryable && i < len(creds)-1 && !firstByte
				moreHops := result.Retryable && hopIndex < len(chain)-1 && !firstByte
				if moreCreds {
					continue
				}
				if moreHops {
					break
				}
				record(fob, req, hop.Provider, hop.Model, started, "error", 0, 0, 0, 0, "")
				return failFromExec(req, hop.Provider, hop.Model, result), nil
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
					if state.Error != "" || !state.Finished {
						status = "error"
						logIncompleteStream(string(hop.Provider), hop.Model, route, state.Error)
					} else {
						httpx.LogOK(string(hop.Provider), hop.Model, route)
					}
					record(fob, req, hop.Provider, hop.Model, started, status, state.PromptTokens, state.CompletionTokens, state.CacheRead, state.CacheWrite, state.RoutedModel)
				}()
				return Result{OK: true, Status: 200, Stream: out}, nil
			}
			body := translate.TranslateResponse(req.Inbound, executor.Format(), hop.Model, req.Body, result.Body)
			pt, ct, cr, cw := usageFrom(body, result.Body)
			httpx.LogOK(string(hop.Provider), hop.Model, route)
			record(fob, req, hop.Provider, hop.Model, started, "ok", pt, ct, cr, cw, routedModel(body, result.Body))
			return Result{OK: true, Status: 200, Body: body}, nil
		}
	}
	if lastHop.Provider == "" {
		lastHop = chain[0]
	}
	if last.Status == 0 {
		if attempted {
			last.Status = 502
			if last.Message == "" {
				last.Message = "all credentials failed"
			}
		} else {
			last.Status = 503
			if last.Message == "" {
				last.Message = "all credentials failed"
			}
		}
	}
	record(fob, req, lastHop.Provider, lastHop.Model, started, "error", 0, 0, 0, 0, "")
	return failFromExec(req, lastHop.Provider, lastHop.Model, last), nil
}

func ListModels(fob *Fob) []domain.ModelInfo {
	creds, _ := fob.Vault.List()
	seenProv := map[domain.ProviderID]bool{}
	var native []domain.ProviderID
	cursorOn := false
	var openaiOn []domain.Credential
	for _, c := range creds {
		if c.Provider == domain.ProviderOpenAI {
			openaiOn = append(openaiOn, c)
			continue
		}
		if seenProv[c.Provider] {
			continue
		}
		seenProv[c.Provider] = true
		if c.Provider == domain.ProviderCursor {
			cursorOn = true
			continue
		}
		native = append(native, c.Provider)
	}
	prefix := false
	listFast := true
	if fob.Settings != nil {
		v, _ := fob.Settings.Get(SettingCursorPrefix)
		prefix = v == "1"
		listFast = settingOnByDefault(fob, SettingCursorListFast)
	}
	seenID := map[string]bool{}
	var models []domain.ModelInfo
	var addMu sync.Mutex
	add := func(list []domain.ModelInfo) {
		addMu.Lock()
		defer addMu.Unlock()
		for _, m := range list {
			if seenID[m.ID] {
				continue
			}
			seenID[m.ID] = true
			models = append(models, m)
		}
	}
	for _, providerID := range native {
		ex := fob.Executors[providerID]
		if ex == nil {
			continue
		}
		add(ex.Models())
	}
	if cursorOn {
		if ex := fob.Executors[domain.ProviderCursor]; ex != nil {
			add(cursorModelsForList(ex, prefix, listFast))
		}
	}
	if live, ok := fob.Executors[domain.ProviderOpenAI].(liveModels); ok && len(openaiOn) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		var wg sync.WaitGroup
		for _, c := range openaiOn {
			wg.Add(1)
			go func(c domain.Credential) {
				defer wg.Done()
				add(live.ModelsFor(ctx, c))
			}(c)
		}
		wg.Wait()
	}
	if models == nil {
		models = []domain.ModelInfo{}
	}
	return models
}

func cursorModelsForList(ex provider.Executor, prefix, listFast bool) []domain.ModelInfo {
	models := ex.Models()
	if !listFast {
		filtered := make([]domain.ModelInfo, 0, len(models))
		for _, m := range models {
			if strings.HasSuffix(cursor.StripPublicPrefix(m.ID), "-fast") {
				continue
			}
			filtered = append(filtered, m)
		}
		models = filtered
	}
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

func pickCredentials(fob *Fob, providerID domain.ProviderID, model, keyID, stickyID string) ([]domain.Credential, error) {
	all, err := fob.Vault.List(providerID)
	if err != nil {
		return nil, err
	}
	if providerID == domain.ProviderOpenAI {
		matched := []domain.Credential{}
		for _, c := range all {
			slug := openai.SourceSlug(c)
			if slug != "" && strings.HasPrefix(model, slug+"/") {
				matched = append(matched, c)
			}
		}
		all = matched
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
	if hops := openaiSourceHops(fob, rawModel); len(hops) > 0 {
		return hops
	}
	forced := strings.HasPrefix(rawModel, "cursor/")
	model := cursor.StripPublicPrefix(rawModel)
	known := cursor.KnownIDs()
	restored := cursor.RestoreWirePrefix(model, known)
	cursorOK := cursorConnected(fob)
	if forced || strings.HasSuffix(model, "-fast") || strings.HasSuffix(restored, "-fast") || strings.HasPrefix(restored, "composer-") || strings.HasPrefix(restored, "cursor-") || strings.HasPrefix(restored, "gemini-") || strings.HasPrefix(restored, "kimi-") || strings.HasPrefix(restored, "glm-") || restored == "cursor-auto" {
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
		if c.Provider == domain.ProviderOpenAI {
			continue
		}
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

func openaiSourceHops(fob *Fob, model string) []hop {
	list, _ := fob.Vault.List(domain.ProviderOpenAI)
	for _, c := range list {
		slug := openai.SourceSlug(c)
		if slug != "" && strings.HasPrefix(model, slug+"/") {
			return []hop{{domain.ProviderOpenAI, model}}
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

func settingOnByDefault(fob *Fob, key string) bool {
	if fob == nil || fob.Settings == nil {
		return true
	}
	v, ok := fob.Settings.Get(key)
	if !ok {
		return true
	}
	return v != "0"
}

func requestedModel(body any) string {
	m, _ := body.(map[string]any)
	s, _ := m["model"].(string)
	return s
}

func nearExpiry(c domain.Credential) bool {
	return c.ExpiresAt != nil && *c.ExpiresAt-time.Now().UnixMilli() < 5*60*1000
}

const keepaliveSkewMS = 5 * 60 * 1000

func KeepaliveCredentials(ctx context.Context, fob *Fob) (int, error) {
	if fob == nil || fob.Vault == nil {
		return 0, nil
	}
	creds, err := fob.Vault.List()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, cred := range creds {
		if !nearExpiry(cred) {
			continue
		}
		ex := fob.Executors[cred.Provider]
		if ex == nil {
			continue
		}
		refreshed, err := ex.Refresh(ctx, cred)
		if err != nil {
			logRefresh(cred.Provider, cred.Label, "keepalive", err)
			continue
		}
		if !credentialAdvanced(cred, refreshed) {
			continue
		}
		if _, err := fob.Vault.Save(store.SaveCredential{
			ID: cred.ID, Provider: cred.Provider, Label: cred.Label, Tokens: refreshed.Tokens, ExpiresAt: refreshed.ExpiresAt,
		}); err != nil {
			httpx.Fail{
				Kind: httpx.KindInternal, Provider: string(cred.Provider), Model: cred.Label,
				Route: "keepalive", Message: "could not save refreshed credential",
			}.Log()
			continue
		}
		n++
	}
	return n, nil
}

func credentialAdvanced(before, after domain.Credential) bool {
	if after.Tokens.AccessToken != "" && after.Tokens.AccessToken != before.Tokens.AccessToken {
		return true
	}
	if after.ExpiresAt == nil {
		return false
	}
	if before.ExpiresAt == nil {
		return true
	}
	return *after.ExpiresAt > *before.ExpiresAt+keepaliveSkewMS/2
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

func record(fob *Fob, req Request, providerID domain.ProviderID, model string, started int64, status string, pt, ct, cr, cw int64, routed string) {
	priceProvider := map[domain.ProviderID]string{
		domain.ProviderClaude: "anthropic",
		domain.ProviderCodex:  "openai",
		domain.ProviderGrok:   "xai",
		domain.ProviderCursor: "cursor",
		domain.ProviderOpenAI: "openai",
	}[providerID]
	meterModel := model
	if routed != "" {
		meterModel = routed
	}
	usd := fob.Prices.Estimate(priceProvider, meterModel, pt, ct, cr, cw)
	if usd == 0 && providerID == domain.ProviderOpenAI {
		if i := strings.Index(meterModel, "/"); i >= 0 {
			usd = fob.Prices.Estimate(priceProvider, meterModel[i+1:], pt, ct, cr, cw)
		}
	}
	_ = fob.Usage.Record(domain.UsageEvent{
		TS: time.Now().UnixMilli(), KeyID: req.Key.ID, Provider: providerID, Model: meterModel, Inbound: req.Inbound,
		PromptTokens: pt, CompletionTokens: ct, CacheReadTokens: cr, CacheWriteTokens: cw,
		LatencyMs: time.Now().UnixMilli() - started, Status: status, USD: usd,
	})
}

func routedModel(translated, upstream any) string {
	for _, src := range []any{translated, upstream} {
		usage := asMap(asMap(src)["usage"])
		if s, _ := usage["routed_model"].(string); s != "" {
			return s
		}
	}
	return ""
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
