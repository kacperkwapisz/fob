package translate

import "github.com/kacperkwapisz/fob/internal/domain"

type requestFn func(model string, stream bool, body any) RequestResult
type responseFn func(model string, original, upstream any) any
type streamFn func(model string, original, chunk any, state *StreamState) []string

var requestFns = map[string]requestFn{
	"openai.chat→claude":      openaiChatToClaude,
	"openai.chat→codex":       openaiChatToCodex,
	"openai.chat→grok":        openaiChatToCodex,
	"claude.messages→claude":  claudeToClaude,
	"claude.messages→codex":   claudeToCodex,
	"claude.messages→grok":    claudeToCodex,
	"openai.responses→claude": responsesToClaude,
	"openai.responses→codex":  responsesToCodex,
	"openai.responses→grok":   responsesToCodex,
	"openai.chat→cursor":      identityChat,
	"claude.messages→cursor":  claudeToGrok,
	"openai.responses→cursor": responsesToGrok,
}

var responseFns = map[string]responseFn{
	"openai.chat→claude":      claudeToOpenaiChat,
	"openai.chat→codex":       codexToOpenaiChat,
	"openai.chat→grok":        codexToOpenaiChat,
	"claude.messages→claude":  claudeFromClaude,
	"claude.messages→codex":   codexToClaude,
	"claude.messages→grok":    codexToClaude,
	"openai.responses→claude": claudeToResponses,
	"openai.responses→codex":  codexToResponses,
	"openai.responses→grok":   codexToResponses,
	"openai.chat→cursor":      grokToOpenaiChat,
	"claude.messages→cursor":  grokToClaude,
	"openai.responses→cursor": grokToResponses,
}

var streamFns = map[string]streamFn{
	"openai.chat→claude":      claudeStreamToOpenaiChat,
	"openai.chat→codex":       codexStreamToOpenaiChat,
	"openai.chat→grok":        codexStreamToOpenaiChat,
	"claude.messages→claude":  claudeStreamIdentity,
	"claude.messages→codex":   codexStreamToClaude,
	"claude.messages→grok":    codexStreamToClaude,
	"openai.responses→claude": claudeStreamToResponses,
	"openai.responses→codex":  codexStreamToResponses,
	"openai.responses→grok":   codexStreamToResponses,
	"openai.chat→cursor":      grokStreamToOpenaiChat,
	"claude.messages→cursor":  grokStreamToClaude,
	"openai.responses→cursor": grokStreamToResponses,
}

func identityChat(model string, stream bool, body any) RequestResult {
	return RequestResult{Model: model, Stream: stream, Body: RewriteModel(body, model)}
}

func TranslateRequest(from domain.InboundFormat, to domain.ExecutorFormat, model string, stream bool, body any) RequestResult {
	key := string(from) + "→" + string(to)
	if fn, ok := requestFns[key]; ok {
		return fn(model, stream, body)
	}
	return RequestResult{Model: model, Stream: stream, Body: RewriteModel(body, model)}
}

func TranslateResponse(from domain.InboundFormat, to domain.ExecutorFormat, model string, original, upstream any) any {
	key := string(from) + "→" + string(to)
	if fn, ok := responseFns[key]; ok {
		return fn(model, original, upstream)
	}
	return upstream
}

func TranslateStream(from domain.InboundFormat, to domain.ExecutorFormat, model string, original, chunk any, state *StreamState) []string {
	key := string(from) + "→" + string(to)
	if fn, ok := streamFns[key]; ok {
		return fn(model, original, chunk, state)
	}
	return []string{"data: " + mustJSON(chunk)}
}

func DoneLine(from domain.InboundFormat) string {
	if from == domain.InboundOpenAIChat {
		return "data: [DONE]"
	}
	return ""
}
