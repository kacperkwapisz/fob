package opencode

import (
	"sort"

	"github.com/kacperkwapisz/fob/internal/domain"
)

func CatalogModels() []domain.ModelInfo {
	rows := loadCatalog()
	out := make([]domain.ModelInfo, 0, len(rows))
	for _, m := range rows {
		if FamilyOf(m.ID) == FamilySkip {
			continue
		}
		out = append(out, modelInfo(m))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func modelInfo(m catalogModel) domain.ModelInfo {
	name := m.Name
	if name == "" {
		name = m.ID
	}
	input := m.Input
	if len(input) == 0 {
		input = []string{"text"}
	}
	info := domain.ModelInfo{
		ID:              PublicID(m.ID),
		Object:          "model",
		OwnedBy:         "opencode",
		Name:            name,
		DisplayName:     name,
		Input:           append([]string{}, input...),
		InputModalities: append([]string{}, input...),
		Reasoning:       m.Reasoning,
		Efforts:         append([]string{}, m.Efforts...),
		Cost:            m.Cost,
	}
	if len(info.Efforts) == 0 {
		for _, opt := range m.ReasoningOptions {
			if opt.Type == "effort" && len(opt.Values) > 0 {
				info.Efforts = append([]string{}, opt.Values...)
				break
			}
		}
	}
	if m.Limit != nil {
		info.ContextLength = m.Limit.Context
		info.MaxOutputTokens = m.Limit.Output
	}
	if info.Reasoning || len(info.Efforts) > 0 {
		info.Reasoning = true
	}
	return info
}

func FilterLive(liveIDs []string) []domain.ModelInfo {
	if len(liveIDs) == 0 {
		return CatalogModels()
	}
	allow := map[string]bool{}
	for _, id := range liveIDs {
		id = StripPrefix(id)
		if id == "" || FamilyOf(id) == FamilySkip {
			continue
		}
		allow[id] = true
	}
	out := []domain.ModelInfo{}
	for _, m := range CatalogModels() {
		if allow[StripPrefix(m.ID)] {
			out = append(out, m)
		}
	}
	return out
}
