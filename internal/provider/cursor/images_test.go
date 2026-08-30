package cursor

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestDecodesDataURL(t *testing.T) {
	png := base64.StdEncoding.EncodeToString([]byte("hello-image"))
	parts, err := ExtractImages([]any{map[string]any{
		"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64," + png},
	}})
	if err != nil || len(parts) != 1 || string(parts[0].Data) != "hello-image" {
		t.Fatalf("%+v %v", parts, err)
	}
}

func TestRejectsOversized(t *testing.T) {
	big := strings.Repeat("A", MaxImageBytes+10)
	enc := base64.StdEncoding.EncodeToString([]byte(big))
	_, err := ExtractImages([]any{map[string]any{
		"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64," + enc},
	}})
	if err == nil {
		t.Fatal("expected size error")
	}
}
