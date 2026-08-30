.PHONY: test build proto tidy

test:
	go test ./...

build:
	CGO_ENABLED=0 go build -o fob ./cmd/fob

tidy:
	go mod tidy

proto:
	mkdir -p internal/provider/cursor/agentpb
	protoc -I proto --go_out=internal/provider/cursor/agentpb --go_opt=paths=source_relative proto/agent/v1/agent.proto
	@if [ -f internal/provider/cursor/agentpb/agent/v1/agent.pb.go ]; then \
		mv internal/provider/cursor/agentpb/agent/v1/agent.pb.go internal/provider/cursor/agentpb/agent.pb.go; \
		rm -rf internal/provider/cursor/agentpb/agent; \
	fi
