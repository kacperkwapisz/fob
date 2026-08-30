package cursor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestParseMessagesGolden(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	wantRaw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "testdata", "golden", "cursor-parse-messages.json"))
	if err != nil {
		t.Fatal(err)
	}
	parsed := ParseMessages([]OpenAIMessage{
		{Role: "system", Content: "terse"},
		{Role: "user", Content: "list files"},
		{Role: "assistant", Content: nil, ToolCalls: []OpenAIToolCall{{ID: "c1", Type: "function", Function: struct{ Name, Arguments string }{"ls", `{"path":"."}`}}}},
		{Role: "tool", ToolCallID: "c1", Content: "a.ts"},
	})
	got, err := json.Marshal(parsed)
	if err != nil {
		t.Fatal(err)
	}
	var want any
	var gotVal any
	if err := json.Unmarshal(wantRaw, &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(got, &gotVal); err != nil {
		t.Fatal(err)
	}
	wantB, _ := json.Marshal(want)
	gotB, _ := json.Marshal(gotVal)
	if string(wantB) != string(gotB) {
		t.Fatalf("got %s\nwant %s", gotB, wantB)
	}
}
