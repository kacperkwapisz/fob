package cursor

import (
	"regexp"
	"strings"
)

var thinkingTagRe = regexp.MustCompile(`(?i)<(/?)(?:think|thinking|reasoning|thought|think_intent)\s*>`)
var partialTagRe = regexp.MustCompile(`(?i)^</?[a-z_]*$`)

const maxThinkingTagLen = 16

type thinkingFilter struct {
	buffer     string
	inThinking bool
}

func newThinkingFilter() *thinkingFilter { return &thinkingFilter{} }

func (f *thinkingFilter) Process(text string) (content, reasoning string) {
	input := f.buffer + text
	f.buffer = ""
	last := 0
	for _, m := range thinkingTagRe.FindAllStringSubmatchIndex(input, -1) {
		before := input[last:m[0]]
		if f.inThinking {
			reasoning += before
		} else {
			content += before
		}
		closing := m[2] >= 0 && m[3] > m[2] && input[m[2]:m[3]] == "/"
		f.inThinking = !closing
		last = m[1]
	}
	rest := input[last:]
	if lt := strings.LastIndex(rest, "<"); lt >= 0 && len(rest)-lt < maxThinkingTagLen && partialTagRe.MatchString(rest[lt:]) {
		f.buffer = rest[lt:]
		before := rest[:lt]
		if f.inThinking {
			reasoning += before
		} else {
			content += before
		}
	} else if f.inThinking {
		reasoning += rest
	} else {
		content += rest
	}
	return
}

func (f *thinkingFilter) Flush() (content, reasoning string) {
	b := f.buffer
	f.buffer = ""
	if b == "" {
		return "", ""
	}
	if f.inThinking {
		return "", b
	}
	return b, ""
}
