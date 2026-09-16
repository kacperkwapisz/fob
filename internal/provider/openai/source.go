package openai

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/kacperkwapisz/fob/internal/domain"
)

const (
	extraBaseURL = "base_url"
	extraSlug    = "slug"
)

var slugRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`)

func ExtraString(extra map[string]any, key string) string {
	if extra == nil {
		return ""
	}
	s, _ := extra[key].(string)
	return s
}

func SourceSlug(c domain.Credential) string {
	if s := ExtraString(c.Tokens.Extra, extraSlug); s != "" {
		return s
	}
	return Slugify(c.Label)
}

func SourceBaseURL(c domain.Credential) string {
	return ExtraString(c.Tokens.Extra, extraBaseURL)
}

func SourceHost(c domain.Credential) string {
	base := SourceBaseURL(c)
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return base
	}
	return u.Host
}

func Slugify(label string) string {
	s := strings.ToLower(strings.TrimSpace(label))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || r == ' ' || r == '.':
			if b.Len() == 0 || lastDash {
				continue
			}
			b.WriteByte('-')
			lastDash = true
		}
	}
	return clipSlug(strings.Trim(b.String(), "-"))
}

func clipSlug(s string) string {
	s = strings.Trim(s, "-")
	if s == "" {
		return "src"
	}
	if len(s) > 48 {
		s = strings.Trim(s[:48], "-")
	}
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		s = "s-" + strings.Trim(s, "-")
		if len(s) > 48 {
			s = strings.Trim(s[:48], "-")
		}
	}
	if !ValidSlug(s) {
		return "src"
	}
	return s
}

func numberedSlug(base string, n int) string {
	suffix := fmt.Sprintf("-%d", n)
	if len(base)+len(suffix) > 48 {
		cut := 48 - len(suffix)
		if cut < 1 {
			cut = 1
		}
		base = strings.Trim(base[:cut], "-")
	}
	if base == "" {
		base = "s"
	}
	return clipSlug(base + suffix)
}

func ValidSlug(s string) bool {
	return slugRe.MatchString(s)
}

func ReservedSlug(s string) bool {
	switch s {
	case "claude", "codex", "grok", "cursor", "openai", "fob", "v1",
		"gpt", "o1", "o3", "o4", "composer", "gemini", "kimi", "glm":
		return true
	default:
		return false
	}
}

func UniqueSlug(existing []domain.Credential, want string) string {
	want = Slugify(want)
	if ReservedSlug(want) {
		want = clipSlug("src-" + want)
	}
	taken := map[string]bool{}
	for _, c := range existing {
		if c.Provider != domain.ProviderOpenAI {
			continue
		}
		taken[SourceSlug(c)] = true
	}
	if !taken[want] {
		return want
	}
	base := want
	for i := 2; i < 100000; i++ {
		if i == 1000 {
			base = "src"
		}
		next := numberedSlug(base, i)
		if ValidSlug(next) && !taken[next] {
			return next
		}
	}
	return numberedSlug("src", len(existing)+2)
}

func NormalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("base URL is required")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("base URL must be an absolute http(s) URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("base URL must be http or https")
	}
	u.Fragment = ""
	u.RawQuery = ""
	path := strings.TrimRight(u.Path, "/")
	for strings.HasSuffix(path, "/v1") {
		path = strings.TrimSuffix(path, "/v1")
		path = strings.TrimRight(path, "/")
	}
	u.Path = path
	u.RawPath = ""
	return strings.TrimRight(u.String(), "/"), nil
}

func JoinURL(base, path string) string {
	return strings.TrimRight(base, "/") + path
}

func PublicModelID(slug, id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return slug
	}
	return slug + "/" + id
}

func StripSlug(slug, id string) string {
	id = strings.TrimSpace(id)
	prefix := slug + "/"
	if strings.HasPrefix(id, prefix) {
		rest := id[len(prefix):]
		if rest != "" {
			return rest
		}
	}
	return id
}
