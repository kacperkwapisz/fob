package domain

type ProviderID string

const (
	ProviderClaude ProviderID = "claude"
	ProviderCodex  ProviderID = "codex"
	ProviderGrok   ProviderID = "grok"
	ProviderCursor ProviderID = "cursor"
)

var Providers = []ProviderID{ProviderClaude, ProviderCodex, ProviderGrok, ProviderCursor}

func IsProviderID(v string) bool {
	switch ProviderID(v) {
	case ProviderClaude, ProviderCodex, ProviderGrok, ProviderCursor:
		return true
	default:
		return false
	}
}

type InboundFormat string

const (
	InboundOpenAIChat      InboundFormat = "openai.chat"
	InboundOpenAIResponses InboundFormat = "openai.responses"
	InboundClaudeMessages  InboundFormat = "claude.messages"
)

type ExecutorFormat string

const (
	FormatClaude ExecutorFormat = "claude"
	FormatCodex  ExecutorFormat = "codex"
	FormatGrok   ExecutorFormat = "grok"
	FormatCursor ExecutorFormat = "cursor"
)

type LocalKey struct {
	ID        string
	Name      string
	Prefix    string
	Providers []ProviderID
	Models    []string
	DailyCap  *int64
	Revoked   bool
	CreatedAt int64
}

type CredentialTokens struct {
	AccessToken  string
	RefreshToken string
	AccountID    string
	Email        string
	Extra        map[string]any
}

type Credential struct {
	ID        string
	Provider  ProviderID
	Label     string
	Tokens    CredentialTokens
	ExpiresAt *int64
	CreatedAt int64
	UpdatedAt int64
}

type UsageEvent struct {
	TS               int64
	KeyID            string
	Provider         ProviderID
	Model            string
	Inbound          InboundFormat
	PromptTokens     int64
	CompletionTokens int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	LatencyMs        int64
	Status           string
	USD              float64
}

type ModelPrice struct {
	Provider   string
	Model      string
	Input      *float64
	Output     *float64
	CacheRead  *float64
	CacheWrite *float64
}

type ModelInfo struct {
	ID          string `json:"id"`
	Object      string `json:"object"`
	OwnedBy     string `json:"owned_by"`
	DisplayName string `json:"display_name,omitempty"`
}
