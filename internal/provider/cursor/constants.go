package cursor

type ClientKind string

const (
	ClientCLI ClientKind = "cli"
	ClientSDK ClientKind = "sdk"

	WebsiteURL       = "https://cursor.com"
	APIBaseURL       = "https://api2.cursor.sh"
	PublicAPIBaseURL = "https://api.cursor.com"
	AvailableModels  = "/aiserver.v1.AiService/AvailableModels"
	ServerConfigPath = "/aiserver.v1.ServerConfigService/GetServerConfig"
	CLIVersion       = "cli-2026.08.25-3e8eec8"
	SDKVersion       = "sdk-1.0.13"
	MCPProvider      = "fob"
	MaxImageBytes    = 1024 * 1024
)

func ClientHeaders(kind ClientKind) map[string]string {
	version := CLIVersion
	if kind == ClientSDK {
		version = SDKVersion
	}
	return map[string]string{
		"x-cursor-client-type":    string(kind),
		"x-cursor-client-version": version,
		"x-ghost-mode":            "true",
	}
}
