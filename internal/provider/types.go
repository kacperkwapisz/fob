package provider

import (
	"context"

	"github.com/kacperkwapisz/fob/internal/domain"
)

type ExecuteOptions struct {
	Stream         bool
	CountTokens    bool
	Compact        bool
	InboundHeaders map[string]string
	CallerKey      string
}

type ExecuteResult struct {
	OK        bool
	Status    int
	Body      any
	Stream    <-chan any
	Retryable bool
	Message   string
}

type Executor interface {
	ID() domain.ProviderID
	Format() domain.ExecutorFormat
	Models() []domain.ModelInfo
	Execute(ctx context.Context, credential domain.Credential, body any, opts ExecuteOptions) (ExecuteResult, error)
	Refresh(ctx context.Context, credential domain.Credential) (domain.Credential, error)
}

func IsRetryableStatus(status int) bool {
	switch status {
	case 401, 408, 429, 500, 502, 503, 504:
		return true
	default:
		return false
	}
}
