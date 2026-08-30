package cursor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/kacperkwapisz/fob/internal/translate"
)

type OpenAIMessage struct {
	Role       string
	Content    any
	ToolCallID string
	ToolCalls  []OpenAIToolCall
}

type OpenAIToolCall struct {
	ID       string
	Type     string
	Function struct {
		Name      string
		Arguments string
	}
}

type ParsedToolResult struct {
	Content string `json:"content"`
	IsError bool   `json:"isError,omitempty"`
}

type ParsedAssistantTextStep struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

type ParsedToolCallStep struct {
	Kind       string            `json:"kind"`
	ToolCallID string            `json:"toolCallId"`
	ToolName   string            `json:"toolName,omitempty"`
	Arguments  map[string]any    `json:"arguments,omitempty"`
	Result     *ParsedToolResult `json:"result,omitempty"`
}

type ParsedTurnStep any

type ParsedTurn struct {
	UserText string           `json:"userText"`
	Steps    []ParsedTurnStep `json:"steps"`
}

type ParsedToolResultRef struct {
	ToolCallID string `json:"toolCallId"`
	Content    string `json:"content"`
}

type ParsedMessages struct {
	SystemPrompt string                `json:"systemPrompt"`
	UserText     string                `json:"userText"`
	Turns        []ParsedTurn          `json:"turns"`
	ToolResults  []ParsedToolResultRef `json:"toolResults"`
}

func ParseMessages(messages []OpenAIMessage) ParsedMessages {
	systemPrompt := "You are a helpful assistant."
	turns := []ParsedTurn{}
	var systemParts []string
	for _, m := range messages {
		if m.Role == "system" {
			systemParts = append(systemParts, textContent(m.Content))
		}
	}
	if len(systemParts) > 0 {
		systemPrompt = strings.Join(systemParts, "\n")
	}
	type runtimeTurn struct {
		ParsedTurn
		toolCallByID                map[string]*ParsedToolCallStep
		sawToolResult               bool
		sawAssistantAfterToolResult bool
	}
	var current *runtimeTurn
	finalize := func() {
		if current == nil {
			return
		}
		turns = append(turns, current.ParsedTurn)
		current = nil
	}
	for _, msg := range messages {
		if msg.Role == "system" {
			continue
		}
		if msg.Role == "user" {
			finalize()
			current = &runtimeTurn{
				ParsedTurn:   ParsedTurn{UserText: textContent(msg.Content)},
				toolCallByID: map[string]*ParsedToolCallStep{},
			}
			continue
		}
		if current == nil {
			continue
		}
		if msg.Role == "assistant" {
			text := textContent(msg.Content)
			if text != "" {
				if current.sawToolResult {
					current.sawAssistantAfterToolResult = true
				}
				current.Steps = append(current.Steps, ParsedAssistantTextStep{Kind: "assistantText", Text: text})
			}
			for _, tc := range msg.ToolCalls {
				step := &ParsedToolCallStep{
					Kind: "toolCall", ToolCallID: tc.ID, ToolName: tc.Function.Name,
					Arguments: parseToolCallArguments(tc.Function.Arguments),
				}
				current.Steps = append(current.Steps, step)
				current.toolCallByID[step.ToolCallID] = step
			}
			continue
		}
		if msg.Role == "tool" {
			id := msg.ToolCallID
			content := textContent(msg.Content)
			if existing := current.toolCallByID[id]; existing != nil {
				existing.Result = &ParsedToolResult{Content: content}
			} else {
				step := &ParsedToolCallStep{Kind: "toolCall", ToolCallID: id, Arguments: map[string]any{}, Result: &ParsedToolResult{Content: content}}
				current.Steps = append(current.Steps, step)
				if id != "" {
					current.toolCallByID[id] = step
				}
			}
			current.sawToolResult = true
		}
	}
	toolResults := []ParsedToolResultRef{}
	userText := ""
	if current != nil {
		var toolCallSteps []*ParsedToolCallStep
		for _, step := range current.Steps {
			if tc, ok := step.(*ParsedToolCallStep); ok {
				toolCallSteps = append(toolCallSteps, tc)
			}
		}
		hasAnyToolResults := false
		for _, s := range toolCallSteps {
			if s.Result != nil {
				hasAnyToolResults = true
			}
		}
		isToolContinuation := false
		if n := len(current.Steps); n > 0 {
			_, isToolContinuation = current.Steps[n-1].(*ParsedToolCallStep)
		}
		if len(current.Steps) == 0 || isToolContinuation {
			userText = current.UserText
			if hasAnyToolResults {
				for _, s := range toolCallSteps {
					if s.Result != nil {
						toolResults = append(toolResults, ParsedToolResultRef{ToolCallID: s.ToolCallID, Content: s.Result.Content})
					}
				}
			}
		} else {
			turns = append(turns, current.ParsedTurn)
		}
	}
	return ParsedMessages{SystemPrompt: systemPrompt, UserText: userText, Turns: turns, ToolResults: toolResults}
}

func textContent(content any) string {
	if s, ok := content.(string); ok {
		return s
	}
	return translate.FlattenTextForCursor(content)
}

func parseToolCallArguments(raw string) map[string]any {
	if raw == "" {
		return map[string]any{}
	}
	var parsed any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return map[string]any{"__raw": raw}
	}
	if m, ok := parsed.(map[string]any); ok {
		return m
	}
	return map[string]any{"value": parsed}
}

func deriveSessionID(piSessionID, user string) string {
	raw := strings.TrimSpace(piSessionID)
	if raw == "" {
		raw = strings.TrimSpace(user)
	}
	return raw
}

func hash16(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:16]
}

func DeriveBridgeKey(messages []OpenAIMessage, sessionID string) string {
	if sessionID != "" {
		return hash16("bridge:" + sessionID)
	}
	first := ""
	for _, m := range messages {
		if m.Role == "user" {
			first = textContent(m.Content)
			break
		}
	}
	if len(first) > 200 {
		first = first[:200]
	}
	return hash16("bridge:" + first)
}

func DeriveConversationKey(messages []OpenAIMessage, sessionID string) string {
	if sessionID != "" {
		return hash16("conv:" + sessionID)
	}
	first := ""
	for _, m := range messages {
		if m.Role == "user" {
			first = textContent(m.Content)
			break
		}
	}
	if len(first) > 200 {
		first = first[:200]
	}
	return hash16("conv:" + first)
}

func DeterministicConversationID(convKey string) string {
	sum := sha256.Sum256([]byte("cursor-conv-id:" + convKey))
	h := hex.EncodeToString(sum[:])[:32]
	nibble, _ := strconv.ParseInt(string(h[16]), 16, 0)
	return h[0:8] + "-" + h[8:12] + "-4" + h[13:16] + "-" + strconv.FormatInt(0x8|(nibble&0x3), 16) + h[17:20] + "-" + h[20:32]
}
