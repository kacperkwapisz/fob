package claude

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cespare/xxhash/v2"
)

const (
	cchSeed = uint64(0x4d659218e32a3268)
	cchLen  = 5
	cchZero = "00000"
)

var excluded = map[string]bool{
	`"max_tokens"`:            true,
	`"fallbacks"`:             true,
	`"fallback_credit_token"`: true,
}

func SignCch(body, fallbackBilling string) string {
	next := ensurePlaceholder(body, fallbackBilling)
	offset, ok := digitsOffset(next)
	if !ok {
		return next
	}
	unsigned := []byte(next)
	copy(unsigned[offset:offset+cchLen], []byte(cchZero))
	normalized, err := normalize(unsigned)
	if err != nil {
		return next
	}
	h := xxhash.NewWithSeed(cchSeed)
	_, _ = h.Write(normalized)
	sum := h.Sum64()
	cch := fmt.Sprintf("%05x", sum&0xfffff)
	copy(unsigned[offset:offset+cchLen], []byte(cch))
	return string(unsigned)
}

type textBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func ensurePlaceholder(body, fallback string) string {
	var parsed map[string]any
	if json.Unmarshal([]byte(body), &parsed) != nil {
		return body
	}
	system := parsed["system"]
	var first any
	if arr, ok := system.([]any); ok && len(arr) > 0 {
		first = arr[0]
	}
	text := ""
	if m, ok := first.(map[string]any); ok {
		text, _ = m["text"].(string)
	}
	if !strings.HasPrefix(text, "x-anthropic-billing-header:") {
		if fallback == "" {
			return body
		}
		body = spliceSystem(body, prependBilling(system, fallback))
	}
	if _, ok := digitsOffset(body); ok {
		return body
	}
	var again map[string]any
	if json.Unmarshal([]byte(body), &again) != nil {
		return body
	}
	sys := again["system"]
	arr, ok := sys.([]any)
	if !ok || len(arr) == 0 {
		return body
	}
	block, ok := arr[0].(map[string]any)
	if !ok {
		return body
	}
	billing, _ := block["text"].(string)
	entry := strings.Index(billing, "cc_entrypoint=")
	if entry < 0 {
		return body
	}
	end := strings.Index(billing[entry:], ";")
	if end < 0 {
		return body
	}
	end += entry
	next := billing[:end+1] + " cch=00000;" + billing[end+1:]
	return strings.Replace(body, jsonQuote(billing), jsonQuote(next), 1)
}

func prependBilling(system any, billing string) []textBlock {
	block := textBlock{Type: "text", Text: billing}
	if s, ok := system.(string); ok {
		return []textBlock{block, {Type: "text", Text: s}}
	}
	if arr, ok := system.([]any); ok {
		out := []textBlock{block}
		for _, item := range arr {
			m, _ := item.(map[string]any)
			out = append(out, textBlock{Type: strOr(m["type"], "text"), Text: strOr(m["text"], "")})
		}
		return out
	}
	return []textBlock{block}
}

func spliceSystem(body string, system []textBlock) string {
	raw, _ := json.Marshal(system)
	trimmed := strings.TrimSpace(body)
	if strings.HasSuffix(trimmed, "}") {
		inner := strings.TrimSpace(trimmed[:len(trimmed)-1])
		if strings.HasSuffix(inner, "{") {
			return inner + `"system":` + string(raw) + "}"
		}
		return inner + `,"system":` + string(raw) + "}"
	}
	return body
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func strOr(v any, fallback string) string {
	s, _ := v.(string)
	if s == "" {
		return fallback
	}
	return s
}

func digitsOffset(body string) (int, bool) {
	var parsed struct {
		System any `json:"system"`
	}
	if json.Unmarshal([]byte(body), &parsed) != nil {
		return 0, false
	}
	arr, ok := parsed.System.([]any)
	if !ok || len(arr) == 0 {
		return 0, false
	}
	first, _ := arr[0].(map[string]any)
	text, _ := first["text"].(string)
	if !strings.HasPrefix(text, "x-anthropic-billing-header:") {
		return 0, false
	}
	quoted, _ := json.Marshal(text)
	inBody := strings.Index(body, string(quoted))
	if inBody < 0 {
		return 0, false
	}
	inner := string(quoted[1 : len(quoted)-1])
	from := 0
	for from < len(inner) {
		rel := strings.Index(inner[from:], "cch=")
		if rel < 0 {
			return 0, false
		}
		rel += from
		digits := rel + 4
		end := digits + cchLen
		if end < len(inner) && inner[end] == ';' && isHex5(inner[digits:end]) {
			return inBody + 1 + digits, true
		}
		from = rel + 4
	}
	return 0, false
}

func isHex5(s string) bool {
	if len(s) != 5 {
		return false
	}
	for i := 0; i < 5; i++ {
		c := s[i]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') {
			continue
		}
		return false
	}
	return true
}

type edit struct{ start, end int }
type member struct {
	start, end, commaBefore, commaAfter int
	excluded                            bool
}

type scanner struct {
	body  []byte
	pos   int
	edits []edit
}

func (s *scanner) parseValue(collect bool) error {
	s.skip()
	if s.pos >= len(s.body) {
		return fmt.Errorf("missing JSON value")
	}
	c := s.body[s.pos]
	if c == '{' {
		return s.parseObject(collect)
	}
	if c == '[' {
		return s.parseArray(collect)
	}
	if c == '"' {
		_, _, err := s.parseString()
		return err
	}
	start := s.pos
	for s.pos < len(s.body) {
		ch := s.body[s.pos]
		if ch == ',' || ch == '}' || ch == ']' || ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' {
			if s.pos == start {
				return fmt.Errorf("missing JSON value")
			}
			return nil
		}
		s.pos++
	}
	return nil
}

func (s *scanner) parseObject(collect bool) error {
	s.pos++
	s.skip()
	if s.consume('}') {
		return nil
	}
	var members []member
	commaBefore := -1
	for {
		s.skip()
		memberStart := s.pos
		keyStart, keyEnd, err := s.parseString()
		if err != nil {
			return err
		}
		s.skip()
		if !s.consume(':') {
			return fmt.Errorf("missing object colon")
		}
		s.skip()
		key := string(s.body[keyStart:keyEnd])
		excludedKey := collect && excluded[key]
		if collect && key == `"model"` && s.pos < len(s.body) && s.body[s.pos] == '"' {
			vs, ve, err := s.parseString()
			if err != nil {
				return err
			}
			s.addEdit(vs+1, ve-1)
		} else if err := s.parseValue(collect && !excludedKey); err != nil {
			return err
		}
		memberEnd := s.pos
		s.skip()
		commaAfter := -1
		if s.consume(',') {
			commaAfter = s.pos - 1
		}
		members = append(members, member{start: memberStart, end: memberEnd, commaBefore: commaBefore, commaAfter: commaAfter, excluded: excludedKey})
		if commaAfter >= 0 {
			commaBefore = commaAfter
			continue
		}
		if !s.consume('}') {
			return fmt.Errorf("missing object end")
		}
		break
	}
	if collect {
		s.addExcluded(members)
	}
	return nil
}

func (s *scanner) parseArray(collect bool) error {
	s.pos++
	s.skip()
	if s.consume(']') {
		return nil
	}
	for {
		if err := s.parseValue(collect); err != nil {
			return err
		}
		s.skip()
		if s.consume(',') {
			continue
		}
		if !s.consume(']') {
			return fmt.Errorf("missing array end")
		}
		return nil
	}
}

func (s *scanner) parseString() (int, int, error) {
	if s.pos >= len(s.body) || s.body[s.pos] != '"' {
		return 0, 0, fmt.Errorf("missing JSON string")
	}
	start := s.pos
	s.pos++
	for s.pos < len(s.body) {
		c := s.body[s.pos]
		if c == '\\' {
			s.pos += 2
			continue
		}
		if c == '"' {
			s.pos++
			return start, s.pos, nil
		}
		s.pos++
	}
	return 0, 0, fmt.Errorf("unterminated JSON string")
}

func (s *scanner) addExcluded(members []member) {
	for start := 0; start < len(members); {
		if !members[start].excluded {
			start++
			continue
		}
		end := start
		for end+1 < len(members) && members[end+1].excluded {
			end++
		}
		if end+1 < len(members) {
			s.addEdit(members[start].start, members[end].commaAfter+1)
		} else if start > 0 && end > start {
			s.addEdit(members[start].start, members[end].end)
		} else if start > 0 {
			s.addEdit(members[start].commaBefore, members[end].end)
		} else {
			s.addEdit(members[start].start, members[end].end)
		}
		start = end + 1
	}
}

func (s *scanner) addEdit(start, end int) {
	if start < end {
		s.edits = append(s.edits, edit{start, end})
	}
}

func (s *scanner) skip() {
	for s.pos < len(s.body) {
		c := s.body[s.pos]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			s.pos++
			continue
		}
		return
	}
}

func (s *scanner) consume(ch byte) bool {
	if s.pos >= len(s.body) || s.body[s.pos] != ch {
		return false
	}
	s.pos++
	return true
}

func normalize(body []byte) ([]byte, error) {
	if !json.Valid(body) {
		return nil, fmt.Errorf("invalid JSON")
	}
	sc := &scanner{body: body}
	if err := sc.parseValue(true); err != nil {
		return nil, err
	}
	sc.skip()
	if sc.pos != len(body) {
		return nil, fmt.Errorf("unexpected JSON data")
	}
	// sort edits by start
	for i := 1; i < len(sc.edits); i++ {
		for j := i; j > 0 && sc.edits[j].start < sc.edits[j-1].start; j-- {
			sc.edits[j], sc.edits[j-1] = sc.edits[j-1], sc.edits[j]
		}
	}
	var out []byte
	last := 0
	for _, e := range sc.edits {
		if e.start < last || e.end > len(body) {
			return nil, fmt.Errorf("overlapping CCH edit")
		}
		out = append(out, body[last:e.start]...)
		last = e.end
	}
	out = append(out, body[last:]...)
	return out, nil
}
