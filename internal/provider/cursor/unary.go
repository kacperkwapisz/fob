package cursor

import (
	"context"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/kacperkwapisz/fob/internal/provider/cursor/agentpb"
)

func CallUnary(ctx context.Context, accessToken, rpcPath string, requestBody []byte, agentURL string, timeout time.Duration) (body []byte, exitCode int, timedOut bool) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if agentURL == "" {
		u, err := ResolveAgentURL(ctx, accessToken, ClientCLI)
		if err != nil {
			return nil, 1, false
		}
		agentURL = u
	}
	factory := currentFactory()
	bridge := factory(accessToken, rpcPath, agentURL, true, ClientCLI)
	var chunks [][]byte
	done := make(chan int, 1)
	bridge.OnData(func(chunk []byte) { chunks = append(chunks, append([]byte(nil), chunk...)) })
	bridge.OnClose(func(code int) { done <- code })
	bridge.Write(requestBody)
	bridge.End()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case code := <-done:
		return joinBytes(chunks), code, false
	case <-timer.C:
		if bridge.Alive != nil && bridge.Alive() {
			bridge.End()
		}
		return joinBytes(chunks), 1, true
	case <-ctx.Done():
		bridge.End()
		return joinBytes(chunks), 1, true
	}
}

func GetUsableModels(ctx context.Context, accessToken string) ([]Model, error) {
	req, _ := proto.Marshal(&agentpb.GetUsableModelsRequest{})
	url, err := ResolveAgentURL(ctx, accessToken, ClientCLI)
	if err != nil {
		return nil, err
	}
	body, code, timedOut := CallUnary(ctx, accessToken, "/agent.v1.AgentService/GetUsableModels", req, url, 5*time.Second)
	if timedOut || code != 0 || len(body) == 0 {
		return nil, nil
	}
	var resp agentpb.GetUsableModelsResponse
	if proto.Unmarshal(body, &resp) != nil {
		if decoded := decodeConnectUnary(body); decoded != nil {
			_ = proto.Unmarshal(decoded, &resp)
		}
	}
	var out []Model
	seen := map[string]bool{}
	for _, m := range resp.GetModels() {
		id := m.GetModelId()
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		name := m.GetDisplayName()
		if name == "" {
			name = m.GetDisplayNameShort()
		}
		if name == "" {
			name = m.GetDisplayModelId()
		}
		if name == "" {
			name = id
		}
		out = append(out, Model{ID: id, Name: name})
	}
	return out, nil
}

func decodeConnectUnary(payload []byte) []byte {
	if len(payload) < 5 {
		return nil
	}
	offset := 0
	for offset+5 <= len(payload) {
		flags := payload[offset]
		msgLen := int(payload[offset+1])<<24 | int(payload[offset+2])<<16 | int(payload[offset+3])<<8 | int(payload[offset+4])
		end := offset + 5 + msgLen
		if end > len(payload) {
			return nil
		}
		if flags&0b0000_0001 != 0 {
			return nil
		}
		if flags&connectEndStreamFlag == 0 {
			return payload[offset+5 : end]
		}
		offset = end
	}
	return nil
}

func joinBytes(chunks [][]byte) []byte {
	n := 0
	for _, c := range chunks {
		n += len(c)
	}
	out := make([]byte, 0, n)
	for _, c := range chunks {
		out = append(out, c...)
	}
	return out
}
