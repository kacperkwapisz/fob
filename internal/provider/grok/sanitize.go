package grok

import (
	"encoding/base64"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/kacperkwapisz/fob/internal/translate"
)

const maxTools = 200

var dropFields = []string{"stop", "prompt_cache_retention", "previous_response_id", "stream_options", "safety_identifier"}

var safeParams = map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": true}

func SanitizeBody(body any, model string) map[string]any {
	out := translate.AsMap(translate.CloneJSON(body))
	out["model"] = model
	for _, f := range dropFields {
		delete(out, f)
	}
	if out["instructions"] == nil {
		out["instructions"] = ""
	}
	flattenMultiAgent(out)
	promoteAdditionalTools(out)
	fold := flattenedCount(translate.AsArr(out["tools"])) > maxTools
	out["tools"] = normalizeTools(translate.AsArr(out["tools"]), model, fold)
	normalizeCustomToolCalls(out)
	qualifyToolChoice(out)
	pruneToolChoice(out)
	tools := translate.AsArr(out["tools"])
	if len(tools) == 0 {
		delete(out, "tools")
		delete(out, "tool_choice")
		delete(out, "parallel_tool_calls")
	} else if len(tools) > maxTools {
		out["tools"] = tools[:maxTools]
		pruneToolChoice(out)
	}
	stripReasoningEffort(out, model)
	sanitizeEncrypted(out)
	return out
}

func flattenMultiAgent(body map[string]any) {
	input := translate.AsArr(body["input"])
	if len(input) == 0 {
		return
	}
	next := make([]any, 0, len(input))
	for _, item := range input {
		rec := translate.AsMap(item)
		if translate.AsStr(rec["type"]) != "agent_message" {
			next = append(next, item)
			continue
		}
		content := translate.AsArr(rec["content"])
		nc := make([]any, len(content))
		for i, part := range content {
			p := translate.AsMap(part)
			if translate.AsStr(p["type"]) != "encrypted_content" {
				nc[i] = part
				continue
			}
			nc[i] = map[string]any{"type": "input_text", "text": translate.AsStr(p["encrypted_content"])}
		}
		cloned := translate.AsMap(translate.CloneJSON(rec))
		cloned["type"] = "message"
		cloned["role"] = "user"
		cloned["content"] = nc
		next = append(next, cloned)
	}
	body["input"] = next
	stripCollabEncryption(body["tools"])
}

func stripCollabEncryption(tools any) {
	var walk func(node any)
	walk = func(node any) {
		rec := translate.AsMap(node)
		name := translate.AsStr(rec["name"])
		if name == "spawn_agent" || name == "send_message" || name == "followup_task" {
			params := translate.AsMap(rec["parameters"])
			props := translate.AsMap(params["properties"])
			message := translate.AsMap(props["message"])
			if _, ok := message["encrypted"]; ok {
				nextMsg := translate.AsMap(translate.CloneJSON(message))
				delete(nextMsg, "encrypted")
				props["message"] = nextMsg
				params["properties"] = props
				rec["parameters"] = params
			}
		}
		if translate.AsStr(rec["type"]) == "namespace" {
			for _, child := range translate.AsArr(rec["tools"]) {
				walk(child)
			}
		}
	}
	for _, tool := range translate.AsArr(tools) {
		walk(tool)
	}
}

func promoteAdditionalTools(body map[string]any) {
	input := translate.AsArr(body["input"])
	if len(input) == 0 {
		return
	}
	var extra, kept []any
	for _, item := range input {
		rec := translate.AsMap(item)
		if translate.AsStr(rec["type"]) == "additional_tools" {
			extra = append(extra, translate.AsArr(rec["tools"])...)
		} else {
			kept = append(kept, item)
		}
	}
	if len(extra) == 0 {
		return
	}
	body["input"] = kept
	body["tools"] = append(translate.AsArr(body["tools"]), extra...)
}

func normalizeCustomToolCalls(body map[string]any) {
	input := translate.AsArr(body["input"])
	if len(input) == 0 {
		return
	}
	var next []any
	for _, item := range input {
		rec := translate.AsMap(item)
		typ := translate.AsStr(rec["type"])
		if typ == "custom_tool_call" {
			callID := translate.AsStr(rec["call_id"])
			name := translate.AsStr(rec["name"])
			if callID == "" || name == "" {
				continue
			}
			next = append(next, map[string]any{"type": "function_call", "call_id": callID, "name": name, "arguments": customArgs(rec["input"])})
			continue
		}
		if typ == "custom_tool_call_output" {
			callID := translate.AsStr(rec["call_id"])
			if callID == "" {
				continue
			}
			out := rec["output"]
			if s, ok := out.(string); ok {
				next = append(next, map[string]any{"type": "function_call_output", "call_id": callID, "output": s})
			} else {
				b, _ := json.Marshal(out)
				if out == nil {
					b = []byte(`""`)
				}
				next = append(next, map[string]any{"type": "function_call_output", "call_id": callID, "output": string(b)})
			}
			continue
		}
		next = append(next, item)
	}
	body["input"] = next
}

func customArgs(input any) string {
	if input == nil {
		return "{}"
	}
	if s, ok := input.(string); ok {
		t := strings.TrimSpace(s)
		if strings.HasPrefix(t, "{") {
			return t
		}
		b, _ := json.Marshal(map[string]any{"input": input})
		return string(b)
	}
	if m, ok := input.(map[string]any); ok {
		b, _ := json.Marshal(m)
		return string(b)
	}
	b, _ := json.Marshal(map[string]any{"input": input})
	return string(b)
}

func flattenedCount(tools []any) int {
	n := 0
	for _, tool := range tools {
		rec := translate.AsMap(tool)
		typ := translate.AsStr(rec["type"])
		if typ == "namespace" {
			c := len(translate.AsArr(rec["tools"]))
			if c < 1 {
				c = 1
			}
			n += c
		} else if typ != "tool_search" {
			n++
		}
	}
	return n
}

func normalizeTools(tools []any, model string, fold bool) []any {
	keepImage := supportsImageGeneration(model)
	var out []any
	for _, tool := range tools {
		out = append(out, flattenTool(translate.AsMap(tool), "", keepImage, fold)...)
	}
	return out
}

func flattenTool(tool map[string]any, namespace string, keepImage, fold bool) []any {
	typ := translate.AsStr(tool["type"])
	if typ == "tool_search" {
		return nil
	}
	if typ == "image_generation" && !keepImage {
		return nil
	}
	if typ == "namespace" {
		if fold {
			if d := dispatcherTool(tool); d != nil {
				return []any{d}
			}
			return nil
		}
		ns := translate.AsStr(tool["name"])
		var nested []any
		for _, child := range translate.AsArr(tool["tools"]) {
			nested = append(nested, flattenTool(translate.AsMap(child), ns, keepImage, fold)...)
		}
		return nested
	}
	if typ == "custom" && translate.AsStr(tool["name"]) == "apply_patch" {
		return nil
	}
	next := translate.AsMap(translate.CloneJSON(tool))
	if typ == "custom" {
		next["type"] = "function"
	}
	if translate.AsStr(next["type"]) == "function" || translate.AsStr(next["type"]) == "custom" {
		params := next["parameters"]
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		next["parameters"] = inlineRefs(params)
		next = stampObjectUnion(next)
		if needsSimplification(next, namespace) {
			next["parameters"] = translate.CloneJSON(safeParams)
			if next["strict"] == true {
				next["strict"] = false
			}
		}
		if translate.AsStr(next["type"]) == "function" && next["parameters"] == nil {
			next["parameters"] = map[string]any{"type": "object", "properties": map[string]any{}}
		}
	}
	if translate.AsStr(next["type"]) == "web_search" {
		delete(next, "external_web_access")
	}
	if translate.AsStr(next["type"]) == "function" && namespace != "" {
		name := qualify(namespace, translate.AsStr(next["name"]))
		if name == "" {
			return nil
		}
		next["name"] = name
	}
	return []any{next}
}

func stampObjectUnion(tool map[string]any) map[string]any {
	params := translate.AsMap(tool["parameters"])
	if translate.AsStr(params["type"]) != "object" {
		return tool
	}
	changed := false
	next := translate.AsMap(translate.CloneJSON(params))
	for _, key := range []string{"anyOf", "oneOf"} {
		union := translate.AsArr(params[key])
		if len(union) == 0 {
			continue
		}
		mapped := make([]any, len(union))
		for i, branch := range union {
			rec := translate.AsMap(branch)
			if _, hasType := rec["type"]; !hasType {
				if _, hasRef := rec["$ref"]; !hasRef {
					changed = true
					nb := translate.AsMap(translate.CloneJSON(rec))
					nb["type"] = "object"
					mapped[i] = nb
					continue
				}
			}
			mapped[i] = branch
		}
		next[key] = mapped
	}
	if !changed {
		return tool
	}
	out := translate.AsMap(translate.CloneJSON(tool))
	out["parameters"] = next
	return out
}

func needsSimplification(tool map[string]any, namespace string) bool {
	if isAutomationUpdate(translate.AsStr(tool["name"]), namespace) {
		return true
	}
	params := translate.AsMap(tool["parameters"])
	for _, key := range []string{"anyOf", "oneOf"} {
		for _, branch := range translate.AsArr(params[key]) {
			rec := translate.AsMap(branch)
			if _, ok := rec["$ref"]; ok || !objectOnly(rec["type"]) {
				return true
			}
		}
	}
	return false
}

func isAutomationUpdate(name, namespace string) bool {
	ns := strings.TrimPrefix(namespace, "mcp__")
	tool := strings.TrimPrefix(name, "mcp__")
	if strings.EqualFold(tool, "automation_update") && (strings.EqualFold(ns, "codex_app") || strings.EqualFold(ns, "codex_apps")) {
		return true
	}
	return strings.EqualFold(tool, "codex_app__automation_update") || strings.EqualFold(tool, "codex_apps__automation_update")
}

func objectOnly(typ any) bool {
	if s, ok := typ.(string); ok {
		return strings.ToLower(s) == "object"
	}
	arr, ok := typ.([]any)
	if !ok || len(arr) == 0 {
		return false
	}
	for _, t := range arr {
		s, ok := t.(string)
		if !ok || strings.ToLower(s) != "object" {
			return false
		}
	}
	return true
}

func qualify(namespace, name string) string {
	ns := strings.TrimSpace(namespace)
	n := strings.TrimSpace(name)
	if ns == "" || n == "" || strings.HasPrefix(n, "mcp__") {
		return n
	}
	prefix := ns
	if !strings.HasSuffix(ns, "__") {
		prefix = ns + "__"
	}
	if strings.HasPrefix(n, prefix) {
		return n
	}
	return prefix + n
}

func qualifyToolChoice(body map[string]any) {
	names := map[string]bool{}
	for _, t := range translate.AsArr(body["tools"]) {
		rec := translate.AsMap(t)
		if translate.AsStr(rec["type"]) == "function" {
			if n := translate.AsStr(rec["name"]); n != "" {
				names[n] = true
			}
		}
	}
	fix := func(choice map[string]any) map[string]any {
		if translate.AsStr(choice["type"]) != "function" {
			return choice
		}
		ns := translate.AsStr(choice["namespace"])
		name := translate.AsStr(choice["name"])
		if ns == "" {
			return choice
		}
		qualified := qualify(ns, name)
		next := translate.AsMap(translate.CloneJSON(choice))
		delete(next, "namespace")
		if names[ns] {
			next["name"] = ns
		} else if names[qualified] {
			next["name"] = qualified
		} else {
			next["name"] = qualified
		}
		return next
	}
	choice := body["tool_choice"]
	if rec, ok := choice.(map[string]any); ok {
		if translate.AsStr(rec["type"]) == "allowed_tools" {
			var tools []any
			for _, t := range translate.AsArr(rec["tools"]) {
				tools = append(tools, fix(translate.AsMap(t)))
			}
			next := translate.AsMap(translate.CloneJSON(rec))
			next["tools"] = tools
			body["tool_choice"] = next
		} else {
			body["tool_choice"] = fix(rec)
		}
	}
}

func pruneToolChoice(body map[string]any) {
	available := availableChoiceKeys(body)
	choice := body["tool_choice"]
	if _, ok := choice.(string); ok || choice == nil {
		return
	}
	rec := translate.AsMap(choice)
	if translate.AsStr(rec["type"]) == "allowed_tools" {
		var kept []any
		for _, t := range translate.AsArr(rec["tools"]) {
			if matchesAvailable(translate.AsMap(t), available) {
				kept = append(kept, t)
			}
		}
		if len(kept) == 0 {
			delete(body, "tool_choice")
		} else {
			next := translate.AsMap(translate.CloneJSON(rec))
			next["tools"] = kept
			body["tool_choice"] = next
		}
		return
	}
	if translate.AsStr(rec["type"]) != "" && !matchesAvailable(rec, available) {
		delete(body, "tool_choice")
	}
}

func availableChoiceKeys(body map[string]any) map[string]bool {
	keys := map[string]bool{}
	for _, tool := range translate.AsArr(body["tools"]) {
		rec := translate.AsMap(tool)
		typ := translate.AsStr(rec["type"])
		if typ == "" {
			continue
		}
		if typ == "function" || typ == "custom" {
			name := translate.AsStr(rec["name"])
			if name != "" {
				keys[typ+":"+name] = true
			}
		} else {
			keys[typ] = true
		}
	}
	return keys
}

func matchesAvailable(choice map[string]any, available map[string]bool) bool {
	typ := translate.AsStr(choice["type"])
	if typ == "" {
		return false
	}
	if typ == "function" || typ == "custom" {
		name := translate.AsStr(choice["name"])
		return name != "" && available[typ+":"+name]
	}
	return available[typ]
}

func dispatcherTool(tool map[string]any) map[string]any {
	namespace := strings.TrimSpace(translate.AsStr(tool["name"]))
	if namespace == "" {
		return nil
	}
	var names, catalog []string
	for _, child := range translate.AsArr(tool["tools"]) {
		rec := translate.AsMap(child)
		name := strings.TrimSpace(translate.AsStr(rec["name"]))
		if name == "" {
			continue
		}
		names = append(names, name)
		desc := translate.AsStr(rec["description"])
		if desc != "" {
			catalog = append(catalog, "- "+name+": "+desc)
		} else {
			catalog = append(catalog, "- "+name)
		}
	}
	desc := translate.AsStr(tool["description"])
	if desc == "" {
		desc = "Tools in namespace " + namespace + "."
	}
	if len(catalog) > 0 {
		desc = desc + "\n\nAvailable tools in this namespace:\n" + strings.Join(catalog, "\n")
	}
	nameProp := map[string]any{
		"type":        "string",
		"description": "Child tool name to execute in namespace " + namespace,
	}
	if len(names) > 0 {
		var enum []any
		for _, n := range names {
			enum = append(enum, n)
		}
		nameProp["enum"] = enum
	}
	return map[string]any{
		"type":        "function",
		"name":        namespace,
		"description": desc,
		"parameters": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": nameProp,
				"arguments": map[string]any{
					"type":                 "object",
					"description":          "Arguments object matching the parameter schema of the selected child tool",
					"additionalProperties": true,
				},
			},
			"required": []any{"name"},
		},
	}
}

func stripReasoningEffort(body map[string]any, model string) {
	if supportsReasoningEffort(model) {
		return
	}
	if body["reasoning"] == nil {
		return
	}
	if rec, ok := body["reasoning"].(map[string]any); ok {
		next := translate.AsMap(translate.CloneJSON(rec))
		delete(next, "effort")
		if len(next) == 0 {
			delete(body, "reasoning")
		} else {
			body["reasoning"] = next
		}
	}
}

func supportsReasoningEffort(model string) bool {
	id := strings.ToLower(model)
	if i := strings.LastIndex(id, "/"); i >= 0 {
		id = id[i+1:]
	}
	return strings.HasPrefix(id, "grok-3-mini") || strings.HasPrefix(id, "grok-4.20-multi-agent") || strings.HasPrefix(id, "grok-4.3") || strings.HasPrefix(id, "grok-4.5") || strings.HasPrefix(id, "grok-4.6")
}

func supportsImageGeneration(model string) bool {
	name := strings.ToLower(model)
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimPrefix(name, "grok-")
	if name == "" || name == "4.20" || strings.HasPrefix(name, "4.20-") {
		return false
	}
	m := regexp.MustCompile(`^(\d+)(?:\.(\d+))?`).FindStringSubmatch(name)
	if m == nil {
		return false
	}
	major, _ := strconv.Atoi(m[1])
	minor := 0
	if m[2] != "" {
		minor, _ = strconv.Atoi(m[2])
	}
	return major > 4 || (major == 4 && minor >= 6)
}

func sanitizeEncrypted(body map[string]any) {
	input := translate.AsArr(body["input"])
	if len(input) == 0 {
		return
	}
	var next []any
	for _, item := range input {
		rec := translate.AsMap(item)
		typ := translate.AsStr(rec["type"])
		if typ != "reasoning" && typ != "compaction" {
			next = append(next, item)
			continue
		}
		enc, has := rec["encrypted_content"]
		if !has {
			next = append(next, item)
			continue
		}
		s, _ := enc.(string)
		if !validGrokBlob(s) {
			if typ == "compaction" {
				continue
			}
			cloned := translate.AsMap(translate.CloneJSON(rec))
			delete(cloned, "encrypted_content")
			next = append(next, cloned)
			continue
		}
		next = append(next, item)
	}
	body["input"] = next
}

func validGrokBlob(raw string) bool {
	if raw == "" || raw != strings.TrimSpace(raw) || strings.Contains(raw, "=") {
		return false
	}
	if ok, _ := regexp.MatchString(`^[A-Za-z0-9+/]+$`, raw); !ok {
		return false
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	return err == nil && len(decoded) >= 32
}

func inlineRefs(schema any) any {
	root, ok := schema.(map[string]any)
	if !ok || schema == nil {
		return schema
	}
	defs := translate.AsMap(root["$defs"])
	if len(defs) == 0 {
		defs = translate.AsMap(root["definitions"])
	}
	seen := map[string]bool{}
	var walk func(node any) any
	walk = func(node any) any {
		if node == nil {
			return node
		}
		if arr, ok := node.([]any); ok {
			out := make([]any, len(arr))
			for i, v := range arr {
				out[i] = walk(v)
			}
			return out
		}
		rec, ok := node.(map[string]any)
		if !ok {
			return node
		}
		ref := translate.AsStr(rec["$ref"])
		if strings.HasPrefix(ref, "#/$defs/") || strings.HasPrefix(ref, "#/definitions/") {
			parts := strings.Split(ref, "/")
			key := parts[len(parts)-1]
			if seen[key] {
				return map[string]any{"type": "object", "properties": map[string]any{}}
			}
			seen[key] = true
			resolved := walk(defs[key])
			if resolved == nil {
				resolved = map[string]any{}
			}
			delete(seen, key)
			rest := translate.AsMap(translate.CloneJSON(rec))
			delete(rest, "$ref")
			merged := translate.AsMap(translate.CloneJSON(resolved))
			for k, v := range rest {
				merged[k] = v
			}
			return merged
		}
		out := map[string]any{}
		for k, v := range rec {
			if k == "$defs" || k == "definitions" {
				continue
			}
			out[k] = walk(v)
		}
		return out
	}
	return walk(root)
}
