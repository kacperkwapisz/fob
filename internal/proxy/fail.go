package proxy

import (
	"strings"

	"github.com/kacperkwapisz/fob/internal/domain"
	"github.com/kacperkwapisz/fob/internal/httpx"
	"github.com/kacperkwapisz/fob/internal/provider"
)

func failOut(req Request, status int, kind, providerID, model, message, hint string) Result {
	inbound := string(req.Inbound)
	f := httpx.Fail{
		Kind: kind, Status: status, Type: httpx.TypeFor(status, inbound),
		Provider: providerID, Model: model, Route: inbound,
		Message: message, Hint: hint,
	}
	f.Log()
	return Result{OK: false, Status: status, Body: f.ClientBody(inbound)}
}

func failFromExec(req Request, providerID domain.ProviderID, model string, result provider.ExecuteResult) Result {
	extracted := result.Message
	if extracted == "" {
		extracted = httpx.ExtractErrorMessage(result.Body)
	}
	kind, typ, message, hint := httpx.Humanize(result.Status, string(providerID), extracted)
	inbound := string(req.Inbound)
	if req.Inbound == domain.InboundClaudeMessages {
		typ = httpx.TypeFor(result.Status, inbound)
	}
	if looksLikeNoCred(extracted) {
		kind = httpx.KindNoCredential
		message = "no " + string(providerID) + " credential is connected"
		hint = "open the panel and login to " + string(providerID)
	}
	if looksLikeMissingProvider(extracted) {
		kind = httpx.KindProvider
		hint = "this build cannot serve " + string(providerID)
	}
	status := result.Status
	if status == 0 {
		status = 502
	}
	f := httpx.Fail{
		Kind: kind, Status: status, Type: typ,
		Provider: string(providerID), Model: model, Route: inbound,
		Message: message, Hint: hint,
	}
	f.Log()
	return Result{OK: false, Status: status, Body: f.ClientBody(inbound)}
}

func logRefresh(providerID domain.ProviderID, label, route string, err error) {
	msg := "credential refresh failed"
	if err != nil {
		if cleaned := httpx.CleanMessage(err.Error()); cleaned != "" {
			msg = cleaned
		}
	}
	httpx.Fail{
		Kind: httpx.KindRefresh, Provider: string(providerID), Model: label,
		Route: route, Message: msg,
		Hint: "reconnect " + string(providerID) + " in the panel",
	}.Log()
}

func looksLikeNoCred(msg string) bool {
	low := strings.ToLower(msg)
	return strings.Contains(low, "credential") && strings.Contains(low, "no ")
}

func looksLikeMissingProvider(msg string) bool {
	return strings.Contains(strings.ToLower(msg), "is not available")
}
