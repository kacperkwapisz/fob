package cursor

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	attachLeft  = regexp.MustCompile(`[-/(\[]$`)
	attachRight = regexp.MustCompile(`^[.,;:!?)\]}]`)
	sentenceEnd = regexp.MustCompile(`[.!?]["')\]]?$`)
	blockStart  = regexp.MustCompile(`^(?:#{1,6}\s|` + "```" + `|~~~|>\s|[-*+]\s|\d+\.\s|(?:-{3,}|\*{3,}|_{3,})(?:\s|$))`)
	leadBreaks  = regexp.MustCompile(`^[\n\r]+`)
	trailBreaks = regexp.MustCompile(`[\n\r]+$`)
)

func stripTrailingBreaks(value string) string {
	return trailBreaks.ReplaceAllString(value, "")
}

func unwrapInternal(text string) string {
	var b strings.Builder
	runes := []rune(text)
	i := 0
	for i < len(runes) {
		if !unicode.IsSpace(runes[i]) {
			// look for wrap: non-space, then whitespace containing newline, then non-space
			j := i + 1
			for j < len(runes) && (runes[j] == ' ' || runes[j] == '\t') {
				j++
			}
			k := j
			for k < len(runes) && (runes[k] == '\n' || runes[k] == '\r') {
				k++
			}
			m := k
			for m < len(runes) && (runes[m] == ' ' || runes[m] == '\t') {
				m++
			}
			if j < k && m < len(runes) && !unicode.IsSpace(runes[m]) {
				left := string(runes[i])
				right := string(runes[m:])
				full := string(runes[i:m])
				b.WriteString(unwrapOne(text, left, right, full, string(runes[:i+1])))
				i = m
				continue
			}
		}
		b.WriteRune(runes[i])
		i++
	}
	return b.String()
}

func unwrapOne(source, left, right, full, leftContext string) string {
	if attachLeft.MatchString(left) || attachRight.MatchString(right) {
		return left
	}
	if blockStart.MatchString(right) {
		return left + "\n\n"
	}
	if sentenceEnd.MatchString(strings.TrimRight(leftContext, " \t")) && startsUpper(right) && regexp.MustCompile(`\n[\r\n]`).MatchString(full) {
		return left + "\n\n"
	}
	return left + " "
}

func startsUpper(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsUpper(r)
}

func JoinAssistantText(previous, incoming string) string {
	if incoming == "" {
		return previous
	}
	if previous == "" {
		return unwrapInternal(incoming)
	}
	lead := leadBreaks.FindString(incoming)
	rest := incoming[len(lead):]
	prevTrail := trailBreaks.FindString(previous)
	prevBody := previous
	if prevTrail != "" {
		prevBody = previous[:len(previous)-len(prevTrail)]
	}
	unwrappedRest := unwrapInternal(rest)
	hadBreak := prevTrail != "" || lead != ""
	if !hadBreak {
		return prevBody + unwrappedRest
	}
	if unwrappedRest == "" {
		if lead != "" {
			return prevBody + lead
		}
		return prevBody + prevTrail
	}
	if blockStart.MatchString(strings.TrimLeft(unwrappedRest, " \t")) {
		return prevBody + "\n\n" + strings.TrimLeft(unwrappedRest, "\n\r")
	}
	if attachLeft.MatchString(prevBody) && len(unwrappedRest) > 0 && !unicode.IsSpace([]rune(unwrappedRest)[0]) {
		return prevBody + unwrappedRest
	}
	if attachRight.MatchString(unwrappedRest) {
		return prevBody + unwrappedRest
	}
	if sentenceEnd.MatchString(strings.TrimRight(prevBody, " \t")) && startsUpper(unwrappedRest) &&
		(strings.Contains(prevTrail, "\n\n") || strings.Contains(lead, "\n\n") || strings.Contains(prevTrail, "\r\n\r\n")) {
		return prevBody + "\n\n" + unwrappedRest
	}
	prevRunes := []rune(prevBody)
	restRunes := []rune(unwrappedRest)
	needsSpace := len(prevRunes) > 0 && !unicode.IsSpace(prevRunes[len(prevRunes)-1]) && len(restRunes) > 0 && !unicode.IsSpace(restRunes[0])
	if needsSpace {
		return prevBody + " " + unwrappedRest
	}
	return prevBody + unwrappedRest
}

type TextJoiner struct{ assembled string }

func NewTextJoiner() *TextJoiner { return &TextJoiner{} }

func (j *TextJoiner) Push(incoming string) string {
	if incoming == "" {
		return ""
	}
	before := stripTrailingBreaks(j.assembled)
	j.assembled = JoinAssistantText(j.assembled, incoming)
	after := stripTrailingBreaks(j.assembled)
	if strings.HasPrefix(after, before) {
		return after[len(before):]
	}
	return after
}

func (j *TextJoiner) Flush() string {
	j.assembled = stripTrailingBreaks(j.assembled)
	return ""
}

func (j *TextJoiner) Reset() { j.assembled = "" }
