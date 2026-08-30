package oauth

import (
	"github.com/kacperkwapisz/fob/internal/domain"
	"github.com/kacperkwapisz/fob/internal/env"
)

func Logins(e *env.Env) map[domain.ProviderID]ProviderLogin {
	return map[domain.ProviderID]ProviderLogin{
		domain.ProviderClaude: ClaudeLogin(e),
		domain.ProviderCodex:  CodexLogin(e),
		domain.ProviderGrok:   GrokLogin(e),
		domain.ProviderCursor: CursorLogin(e),
	}
}
