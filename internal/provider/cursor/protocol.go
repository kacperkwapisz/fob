package cursor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/kacperkwapisz/fob/internal/provider"
	"github.com/kacperkwapisz/fob/internal/provider/cursor/agentpb"
	"github.com/kacperkwapisz/fob/internal/translate"
)

type ChatResult struct {
	Status  int
	Body    any
	Stream  <-chan any
	Message string
}

type storedConversation struct {
	conversationID    string
	checkpoint        []byte
	sessionScoped     bool
	blobStore         map[string][]byte
	lastAccess        time.Time
	resumeRequestHash string
}

type activeBridge struct {
	bridge    *Bridge
	stopHeart chan struct{}
	blobStore map[string][]byte
	mcpTools  []*agentpb.McpToolDefinition
	pending   []pendingExec
	current   *ParsedTurn
}

var (
	sessionMu          sync.Mutex
	conversationStates = map[string]*storedConversation{}
	activeBridges      = map[string]*activeBridge{}
)

const conversationTTL = 30 * time.Minute

func CleanupAllSessionState() {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	for _, a := range activeBridges {
		cleanupActive(a)
	}
	activeBridges = map[string]*activeBridge{}
	conversationStates = map[string]*storedConversation{}
}

func cleanupActive(a *activeBridge) {
	if a == nil {
		return
	}
	if a.stopHeart != nil {
		select {
		case <-a.stopHeart:
		default:
			close(a.stopHeart)
		}
	}
	if a.bridge != nil && a.bridge.Alive != nil && a.bridge.Alive() {
		a.bridge.Write(cancelBytes())
		a.bridge.End()
	}
}

func messagesFromBody(body map[string]any) []OpenAIMessage {
	var out []OpenAIMessage
	for _, raw := range translate.AsArr(body["messages"]) {
		m := translate.AsMap(raw)
		msg := OpenAIMessage{Role: translate.AsStr(m["role"]), Content: m["content"], ToolCallID: translate.AsStr(m["tool_call_id"])}
		for _, tc := range translate.AsArr(m["tool_calls"]) {
			c := translate.AsMap(tc)
			fn := translate.AsMap(c["function"])
			call := OpenAIToolCall{ID: translate.AsStr(c["id"]), Type: translate.AsStr(c["type"])}
			call.Function.Name = translate.AsStr(fn["name"])
			call.Function.Arguments = translate.AsStr(fn["arguments"])
			msg.ToolCalls = append(msg.ToolCalls, call)
		}
		out = append(out, msg)
	}
	return out
}

func resolveModelID(model, effort string, fast ...bool) string {
	base := model
	hasFast := strings.HasSuffix(base, "-fast")
	if hasFast {
		base = strings.TrimSuffix(base, "-fast")
	}
	wantFast := hasFast
	if len(fast) > 0 {
		wantFast = fast[0]
	}
	if variants := lookupVariants(base); variants != nil {
		pair := variants[effort]
		if pick := pickVariant(pair, wantFast); pick != "" {
			return pick
		}
	}
	known := KnownIDs()
	if effort != "" {
		candidate := base + "-" + effort
		if wantFast {
			if hit := firstKnown(known, candidate+"-fast", candidate); hit != "" {
				return hit
			}
		} else if hit := firstKnown(known, candidate, candidate+"-fast"); hit != "" {
			return hit
		}
		base = candidate
	} else if mapped := MapNativeToWire(model, known); mapped != "" {
		return mapped
	}
	if wantFast && !strings.HasSuffix(base, "-fast") {
		base += "-fast"
	}
	return base
}

func pickVariant(pair variantPair, wantFast bool) string {
	if wantFast {
		if pair.fast != "" {
			return pair.fast
		}
		return pair.standard
	}
	if pair.standard != "" {
		return pair.standard
	}
	return pair.fast
}

func firstKnown(known []string, ids ...string) string {
	set := map[string]bool{}
	for _, k := range known {
		set[k] = true
	}
	for _, id := range ids {
		if set[id] {
			return id
		}
	}
	return ""
}

func selectionFromBody(body map[string]any) *requestedModelSelection {
	raw := translate.AsMap(body["cursor_requested_model"])
	id := translate.AsStr(raw["modelId"])
	if id == "" {
		id = translate.AsStr(raw["model_id"])
	}
	if id != "" {
		sel := &requestedModelSelection{ModelID: id, MaxMode: raw["maxMode"] == true || raw["max_mode"] == true}
		for _, p := range translate.AsArr(raw["parameters"]) {
			pm := translate.AsMap(p)
			sel.Parameters = append(sel.Parameters, struct{ ID, Value string }{translate.AsStr(pm["id"]), translate.AsStr(pm["value"])})
		}
		return sel
	}
	fastFlag, hasFastFlag := body["fast"].(bool)
	if hasFastFlag {
		return resolveRequestedModel(translate.AsStr(body["model"]), translate.AsStr(body["reasoning_effort"]), fastFlag)
	}
	return resolveRequestedModel(translate.AsStr(body["model"]), translate.AsStr(body["reasoning_effort"]))
}

func resolveRequestedModel(model, effort string, fast ...bool) *requestedModelSelection {
	base := model
	hasFast := strings.HasSuffix(base, "-fast")
	if hasFast {
		base = strings.TrimSuffix(base, "-fast")
	}
	wantFast := hasFast
	if len(fast) > 0 {
		wantFast = fast[0]
	}
	variants := lookupVariants(base)
	if variants == nil {
		return nil
	}
	pair := variants[effort]
	if pair.standard == "" && pair.fast == "" {
		return nil
	}
	wire := pair.standard
	if wantFast && pair.fast != "" {
		wire = pair.fast
	} else if wire == "" {
		wire = pair.fast
	}
	if wire == "" {
		return nil
	}
	var params []struct{ ID, Value string }
	paramBase := base
	if strings.HasSuffix(paramBase, "-thinking") {
		paramBase = strings.TrimSuffix(paramBase, "-thinking")
		params = append(params, struct{ ID, Value string }{"thinking", "true"})
	}
	if effort != "" {
		params = append(params, struct{ ID, Value string }{"effort", effort})
	}
	hasFastTwin := false
	for _, v := range variants {
		if v.fast != "" {
			hasFastTwin = true
			break
		}
	}
	if hasFastTwin {
		params = append(params, struct{ ID, Value string }{"fast", boolString(wire == pair.fast)})
	}
	return &requestedModelSelection{ModelID: paramBase, Parameters: params}
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func evictStale(now time.Time) {
	for k, st := range conversationStates {
		if !st.sessionScoped && now.Sub(st.lastAccess) > conversationTTL {
			delete(conversationStates, k)
		}
	}
}

func RunChat(ctx context.Context, accessToken string, body map[string]any, stream bool, client ClientKind) (ChatResult, error) {
	messages := messagesFromBody(body)
	parsed := ParseMessages(messages)
	fastFlag, hasFastFlag := body["fast"].(bool)
	modelID := resolveModelID(translate.AsStr(body["model"]), translate.AsStr(body["reasoning_effort"]))
	if hasFastFlag {
		modelID = resolveModelID(translate.AsStr(body["model"]), translate.AsStr(body["reasoning_effort"]), fastFlag)
	}
	if parsed.UserText == "" && len(parsed.ToolResults) == 0 {
		return ChatResult{Status: 400, Body: map[string]any{"error": map[string]any{"message": "No user message found", "type": "invalid_request_error"}}, Message: "No user message found"}, nil
	}

	sessionID := deriveSessionID(translate.AsStr(body["pi_session_id"]), translate.AsStr(body["user"]))
	bridgeKey := DeriveBridgeKey(messages, sessionID)
	convKey := DeriveConversationKey(messages, sessionID)

	sessionMu.Lock()
	active := activeBridges[bridgeKey]
	if active != nil && len(parsed.ToolResults) > 0 {
		delete(activeBridges, bridgeKey)
		sessionMu.Unlock()
		if active.bridge.Alive != nil && active.bridge.Alive() {
			return resumeTools(ctx, active, parsed, modelID, bridgeKey, convKey, stream)
		}
		cleanupActive(active)
		sessionMu.Lock()
	} else if active != nil {
		cleanupActive(active)
		delete(activeBridges, bridgeKey)
	}
	stored := conversationStates[convKey]
	if stored == nil {
		stored = &storedConversation{
			conversationID: DeterministicConversationID(convKey),
			sessionScoped:  sessionID != "",
			blobStore:      map[string][]byte{},
			lastAccess:     time.Now(),
		}
		conversationStates[convKey] = stored
	}
	stored.lastAccess = time.Now()
	evictStale(time.Now())
	sessionMu.Unlock()

	mcpTools := buildMcpTools(translate.AsArr(body["tools"]))
	userText := parsed.UserText
	if userText == "" {
		var parts []string
		for _, r := range parsed.ToolResults {
			parts = append(parts, r.Content)
		}
		userText = strings.Join(parts, "\n")
	}
	hash := requestHashOf(translate.AsStr(body["model"]), body["messages"], body["tools"], body["cursor_requested_model"])
	resume := len(stored.checkpoint) > 0 && stored.resumeRequestHash == hash
	var images []ImagePart
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			images, _ = ExtractImages(messages[i].Content)
			break
		}
	}
	payload := buildCursorRequest(modelID, parsed.SystemPrompt, userText, parsed.Turns, stored.conversationID, stored.checkpoint, stored.blobStore, selectionFromBody(body), mcpTools, resume, images)
	current := &ParsedTurn{UserText: userText}
	return runStream(ctx, accessToken, payload, modelID, bridgeKey, convKey, current, stream, hash, client)
}

func runStream(ctx context.Context, accessToken string, payload requestPayload, modelID, bridgeKey, convKey string, current *ParsedTurn, stream bool, requestHash string, client ClientKind) (ChatResult, error) {
	factory := currentFactory()
	agentURL, err := ResolveAgentURL(ctx, accessToken, client)
	if err != nil {
		agentURL = "https://agentn.test.cursor.sh"
	}
	bridge := factory(accessToken, "/agent.v1.AgentService/Run", agentURL, false, client)
	stopHeart := make(chan struct{})
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stopHeart:
				return
			case <-t.C:
				bridge.Write(heartbeatBytes())
			}
		}
	}()

	out := make(chan any, 32)
	var chunks []map[string]any
	completionID := "chatcmpl-" + randHex(14)
	created := time.Now().Unix()
	joiner := NewTextJoiner()
	tags := newThinkingFilter()
	state := &streamProtoState{}
	var parser frameParser
	done := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(done) }) }
	mcpSeen := false
	cancelled := false
	var latestCheckpoint []byte

	emit := func(delta map[string]any, finishReason any) {
		chunk := map[string]any{
			"id": completionID, "object": "chat.completion.chunk", "created": created, "model": modelID,
			"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finishReason}},
		}
		chunks = append(chunks, chunk)
		if stream {
			out <- chunk
		}
	}
	emitUsage := func() {
		chunk := map[string]any{
			"id": completionID, "object": "chat.completion.chunk", "created": created, "model": modelID,
			"choices": []any{}, "usage": computeUsage(state),
		}
		chunks = append(chunks, chunk)
		if stream {
			out <- chunk
		}
	}
	emitAssistant := func(text string) {
		content := joiner.Push(text)
		if content == "" {
			return
		}
		appendAssistantText(current, content)
		emit(map[string]any{"content": content}, nil)
	}

	bridge.OnData(func(incoming []byte) {
		parser.Push(incoming, func(msg []byte) {
			var server agentpb.AgentServerMessage
			if err := proto.Unmarshal(msg, &server); err != nil {
				return
			}
			processServer(&server, payload.blobStore, payload.mcpTools, bridge, state,
				func(text string, thinking bool) {
					if thinking {
						emit(map[string]any{"reasoning_content": text}, nil)
						return
					}
					content, reasoning := tags.Process(text)
					if reasoning != "" {
						emit(map[string]any{"reasoning_content": reasoning}, nil)
					}
					if content != "" {
						emitAssistant(content)
					}
				},
				func(exec pendingExec) {
					state.pending = append(state.pending, exec)
					mcpSeen = true
					c, r := tags.Flush()
					if r != "" {
						emit(map[string]any{"reasoning_content": r}, nil)
					}
					if c != "" {
						emitAssistant(c)
					}
					joiner.Reset()
					current.Steps = append(current.Steps, &ParsedToolCallStep{
						Kind: "toolCall", ToolCallID: exec.toolCallID, ToolName: exec.toolName,
						Arguments: parseToolCallArguments(exec.decodedArgs),
					})
					idx := state.toolIndex
					state.toolIndex++
					emit(map[string]any{"tool_calls": []any{map[string]any{
						"index": idx, "id": exec.toolCallID, "type": "function",
						"function": map[string]any{"name": exec.toolName, "arguments": exec.decodedArgs},
					}}}, nil)
					sessionMu.Lock()
					activeBridges[bridgeKey] = &activeBridge{
						bridge: bridge, stopHeart: stopHeart, blobStore: payload.blobStore,
						mcpTools: payload.mcpTools, pending: state.pending, current: current,
					}
					sessionMu.Unlock()
					emit(map[string]any{}, "tool_calls")
					if stream {
						out <- map[string]any{"done": true}
					}
					finish()
				},
				func(cp []byte) {
					latestCheckpoint = append([]byte(nil), cp...)
					if state.turnEnded {
						state.terminalCheckpoint = true
					}
					sessionMu.Lock()
					st := conversationStates[convKey]
					if st != nil {
						st.checkpoint = append([]byte(nil), cp...)
						if state.turnEnded {
							st.resumeRequestHash = ""
						} else {
							st.resumeRequestHash = requestHash
						}
						for k, v := range payload.blobStore {
							st.blobStore[k] = v
						}
						st.lastAccess = time.Now()
					}
					sessionMu.Unlock()
				},
				func(message string) {
					if cancelled {
						return
					}
					emit(map[string]any{"content": message}, "error")
					emitUsage()
					finish()
				},
			)
		}, func(end []byte) {
			if err := parseConnectEnd(end); err != nil {
				emit(map[string]any{"content": err.Error()}, "error")
				emitUsage()
				finish()
			}
		})
	})
	bridge.OnClose(func(code int) {
		select {
		case <-stopHeart:
		default:
			close(stopHeart)
		}
		sessionMu.Lock()
		st := conversationStates[convKey]
		if st != nil {
			for k, v := range payload.blobStore {
				st.blobStore[k] = v
			}
			st.lastAccess = time.Now()
			if !cancelled && len(latestCheckpoint) > 0 {
				st.checkpoint = append([]byte(nil), latestCheckpoint...)
				if state.turnEnded {
					st.resumeRequestHash = ""
				} else {
					st.resumeRequestHash = requestHash
				}
			}
		}
		sessionMu.Unlock()
		if cancelled || mcpSeen {
			if mcpSeen && code != 0 {
				emit(map[string]any{"content": "Bridge connection lost"}, "error")
				emitUsage()
			}
			finish()
			return
		}
		c, r := tags.Flush()
		if r != "" {
			emit(map[string]any{"reasoning_content": r}, nil)
		}
		if c != "" {
			emitAssistant(c)
		}
		joiner.Flush()
		if !state.turnEnded || (code != 0 && !state.terminalCheckpoint) {
			msg := "Cursor stream ended before turnEnded"
			if state.turnEnded {
				msg = "Cursor stream ended after turnEnded without a terminal checkpoint"
			}
			emit(map[string]any{"content": msg}, "error")
		} else {
			emit(map[string]any{}, "stop")
		}
		emitUsage()
		finish()
	})

	bridge.Write(frameConnect(payload.requestBytes, 0))

	go func() {
		select {
		case <-ctx.Done():
			cancelled = true
			cleanupActive(&activeBridge{bridge: bridge, stopHeart: stopHeart})
			finish()
		case <-done:
		}
		if stream {
			close(out)
		}
	}()

	if stream {
		wrapped := make(chan any, 32)
		go func() {
			defer close(wrapped)
			for ev := range out {
				if m, ok := ev.(map[string]any); ok && m["done"] == true {
					continue
				}
				wrapped <- ev
			}
		}()
		return ChatResult{Status: 200, Stream: wrapped}, nil
	}
	<-done
	if ctx.Err() != nil {
		return ChatResult{Status: 499, Message: "cancelled"}, ctx.Err()
	}
	return collectNonStream(completionID, created, modelID, chunks, state), nil
}

func resumeTools(ctx context.Context, active *activeBridge, parsed ParsedMessages, modelID, bridgeKey, convKey string, stream bool) (ChatResult, error) {
	results := map[string]string{}
	for _, r := range parsed.ToolResults {
		results[r.ToolCallID] = r.Content
		for _, step := range active.current.Steps {
			if tc, ok := step.(*ParsedToolCallStep); ok && tc.ToolCallID == r.ToolCallID {
				tc.Result = &ParsedToolResult{Content: r.Content}
			}
		}
	}
	var unresolved []pendingExec
	for _, exec := range active.pending {
		if _, ok := results[exec.toolCallID]; !ok {
			unresolved = append(unresolved, exec)
		}
	}
	if len(unresolved) > 0 {
		sessionMu.Lock()
		activeBridges[bridgeKey] = active
		sessionMu.Unlock()
		return pendingToolResult(modelID, unresolved, stream), nil
	}
	for _, exec := range active.pending {
		if content, ok := results[exec.toolCallID]; ok {
			sendMcpResult(active.bridge, exec, content)
		}
	}
	// Continue the same stream after tool results.
	payload := requestPayload{blobStore: active.blobStore, mcpTools: active.mcpTools}
	_ = payload
	out := make(chan any, 32)
	completionID := "chatcmpl-" + randHex(14)
	created := time.Now().Unix()
	joiner := NewTextJoiner()
	tags := newThinkingFilter()
	state := &streamProtoState{}
	var parser frameParser
	done := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(done) }) }
	emit := func(delta map[string]any, finishReason any) {
		chunk := map[string]any{
			"id": completionID, "object": "chat.completion.chunk", "created": created, "model": modelID,
			"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finishReason}},
		}
		if stream {
			out <- chunk
		}
	}
	active.bridge.OnData(func(incoming []byte) {
		parser.Push(incoming, func(msg []byte) {
			var server agentpb.AgentServerMessage
			if proto.Unmarshal(msg, &server) != nil {
				return
			}
			processServer(&server, active.blobStore, active.mcpTools, active.bridge, state,
				func(text string, thinking bool) {
					if thinking {
						emit(map[string]any{"reasoning_content": text}, nil)
						return
					}
					c, r := tags.Process(text)
					if r != "" {
						emit(map[string]any{"reasoning_content": r}, nil)
					}
					if c != "" {
						joined := joiner.Push(c)
						if joined != "" {
							emit(map[string]any{"content": joined}, nil)
						}
					}
				},
				func(exec pendingExec) {
					state.pending = append(state.pending, exec)
					sessionMu.Lock()
					active.pending = state.pending
					activeBridges[bridgeKey] = active
					sessionMu.Unlock()
					emit(map[string]any{"tool_calls": []any{map[string]any{
						"index": state.toolIndex, "id": exec.toolCallID, "type": "function",
						"function": map[string]any{"name": exec.toolName, "arguments": exec.decodedArgs},
					}}}, "tool_calls")
					finish()
				},
				func(cp []byte) {
					sessionMu.Lock()
					if st := conversationStates[convKey]; st != nil {
						st.checkpoint = append([]byte(nil), cp...)
						st.lastAccess = time.Now()
					}
					sessionMu.Unlock()
				},
				func(message string) {
					emit(map[string]any{"content": message}, "error")
					finish()
				},
			)
		}, func([]byte) { finish() })
	})
	active.bridge.OnClose(func(int) {
		if state.turnEnded {
			emit(map[string]any{}, "stop")
		}
		finish()
	})
	go func() {
		select {
		case <-ctx.Done():
			cleanupActive(active)
			finish()
		case <-done:
		}
		if stream {
			close(out)
		}
	}()
	if stream {
		return ChatResult{Status: 200, Stream: out}, nil
	}
	<-done
	return ChatResult{Status: 200, Body: map[string]any{"id": completionID, "object": "chat.completion", "model": modelID}}, nil
}

func pendingToolResult(modelID string, pending []pendingExec, stream bool) ChatResult {
	id := "chatcmpl-" + randHex(14)
	created := time.Now().Unix()
	var toolCalls []any
	for i, exec := range pending {
		toolCalls = append(toolCalls, map[string]any{
			"index": i, "id": exec.toolCallID, "type": "function",
			"function": map[string]any{"name": exec.toolName, "arguments": exec.decodedArgs},
		})
	}
	if stream {
		ch := make(chan any, 8)
		go func() {
			defer close(ch)
			for _, tc := range toolCalls {
				ch <- map[string]any{
					"id": id, "object": "chat.completion.chunk", "created": created, "model": modelID,
					"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"tool_calls": []any{tc}}, "finish_reason": nil}},
				}
			}
			ch <- map[string]any{
				"id": id, "object": "chat.completion.chunk", "created": created, "model": modelID,
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "tool_calls"}},
			}
		}()
		return ChatResult{Status: 200, Stream: ch}
	}
	return ChatResult{Status: 200, Body: map[string]any{
		"id": id, "object": "chat.completion", "created": created, "model": modelID,
		"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": nil, "tool_calls": toolCalls}, "finish_reason": "tool_calls"}},
		"usage":   map[string]any{"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0},
	}}
}

func collectNonStream(id string, created int64, model string, chunks []map[string]any, state *streamProtoState) ChatResult {
	var text strings.Builder
	var toolCalls []any
	finish := "stop"
	for _, c := range chunks {
		choice := translate.AsMap(translate.AsArr(c["choices"])[0])
		delta := translate.AsMap(choice["delta"])
		if s := translate.AsStr(delta["content"]); s != "" {
			text.WriteString(s)
		}
		if tc := translate.AsArr(delta["tool_calls"]); len(tc) > 0 {
			toolCalls = append(toolCalls, tc...)
			finish = "tool_calls"
		}
		if fr := translate.AsStr(choice["finish_reason"]); fr != "" {
			finish = fr
		}
	}
	msg := map[string]any{"role": "assistant", "content": text.String()}
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
		msg["content"] = nil
	}
	return ChatResult{Status: 200, Body: map[string]any{
		"id": id, "object": "chat.completion", "created": created, "model": model,
		"choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": finish}},
		"usage":   computeUsage(state),
	}}
}

func appendAssistantText(turn *ParsedTurn, text string) {
	if text == "" || turn == nil {
		return
	}
	if n := len(turn.Steps); n > 0 {
		if last, ok := turn.Steps[n-1].(ParsedAssistantTextStep); ok {
			last.Text += text
			turn.Steps[n-1] = last
			return
		}
	}
	turn.Steps = append(turn.Steps, ParsedAssistantTextStep{Kind: "assistantText", Text: text})
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)[:n]
}

func marshalJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

var _ = provider.IsRetryableStatus
