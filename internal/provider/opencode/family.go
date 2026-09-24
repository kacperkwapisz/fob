package opencode

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/kacperkwapisz/fob/internal/domain"
	"github.com/kacperkwapisz/fob/internal/prices"
)

type Family string

const (
	FamilyChat      Family = "chat"
	FamilyResponses Family = "responses"
	FamilyMessages  Family = "messages"
	FamilySkip      Family = "skip"
)

type catalogModel struct {
	ID               string                `json:"id"`
	Name             string                `json:"name"`
	Limit            *catalogLimit         `json:"limit"`
	Cost             *domain.ModelCostJSON `json:"cost"`
	Provider         *catalogProvider      `json:"provider"`
	Input            []string              `json:"input"`
	Reasoning        bool                  `json:"reasoning"`
	Efforts          []string              `json:"efforts"`
	ReasoningOptions []catalogReasoningOpt `json:"reasoning_options"`
}

type catalogLimit struct {
	Context int `json:"context"`
	Output  int `json:"output"`
	Input   int `json:"input"`
}

type catalogProvider struct {
	NPM string `json:"npm"`
}

type catalogReasoningOpt struct {
	Type   string   `json:"type"`
	Values []string `json:"values"`
}

var (
	catalogOnce sync.Once
	catalogRows []catalogModel
)

func loadCatalog() []catalogModel {
	catalogOnce.Do(func() {
		var top map[string]json.RawMessage
		if json.Unmarshal(prices.OpenCodeJSON, &top) != nil {
			return
		}
		raw, ok := top["opencode"]
		if !ok {
			return
		}
		var pack struct {
			Models map[string]catalogModel `json:"models"`
		}
		if json.Unmarshal(raw, &pack) != nil {
			return
		}
		out := make([]catalogModel, 0, len(pack.Models))
		for id, m := range pack.Models {
			if m.ID == "" {
				m.ID = id
			}
			out = append(out, m)
		}
		catalogRows = out
	})
	return catalogRows
}

func FamilyOf(model string) Family {
	id := StripPrefix(model)
	if id == "" || strings.HasPrefix(id, "jev-") || strings.HasPrefix(id, "test") {
		return FamilySkip
	}
	for _, m := range loadCatalog() {
		if m.ID != id {
			continue
		}
		return familyFromNPM(m.Provider)
	}
	// Unknown to seed: keep callable only if live list included it and it is not gemini/jev.
	if strings.HasPrefix(id, "gemini-") {
		return FamilySkip
	}
	return FamilyChat
}

func familyFromNPM(p *catalogProvider) Family {
	if p == nil || p.NPM == "" {
		return FamilyChat
	}
	switch p.NPM {
	case "@ai-sdk/openai":
		return FamilyResponses
	case "@ai-sdk/anthropic":
		return FamilyMessages
	case "@ai-sdk/google":
		return FamilySkip
	default:
		return FamilyChat
	}
}

func FormatForFamily(f Family) domain.ExecutorFormat {
	switch f {
	case FamilyResponses:
		return domain.FormatCodex
	case FamilyMessages:
		return domain.FormatClaude
	default:
		return domain.FormatOpenAI
	}
}

func StripPrefix(id string) string {
	id = strings.TrimSpace(id)
	if strings.HasPrefix(id, "opencode/") {
		return strings.TrimPrefix(id, "opencode/")
	}
	return id
}

func PublicID(id string) string {
	id = StripPrefix(id)
	if id == "" {
		return ""
	}
	return "opencode/" + id
}
