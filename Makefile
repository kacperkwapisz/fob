.PHONY: test signoff build run tidy proto fmt vet clean

test:
	go test ./...

signoff: test
	gh signoff create

build:
	CGO_ENABLED=0 go build -trimpath -o fob ./cmd/fob

run:
	go run ./cmd/fob

tidy:
	go mod tidy

fmt:
	gofmt -w .

vet:
	go vet ./...

proto:
	mkdir -p internal/provider/cursor/agentpb
	protoc -I proto --go_out=internal/provider/cursor/agentpb --go_opt=paths=source_relative proto/agent/v1/agent.proto
	if [ -f internal/provider/cursor/agentpb/agent/v1/agent.pb.go ]; then \
		mv internal/provider/cursor/agentpb/agent/v1/agent.pb.go internal/provider/cursor/agentpb/agent.pb.go; \
		rm -rf internal/provider/cursor/agentpb/agent; \
	fi

clean:
	rm -f fob
	rm -rf dist
