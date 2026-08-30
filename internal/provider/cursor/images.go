package cursor

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/kacperkwapisz/fob/internal/translate"
)

type ImagePart struct {
	Data     []byte
	MimeType string
	UUID     string
}

func ExtractImages(content any) ([]ImagePart, error) {
	if _, ok := content.(string); ok || content == nil {
		return nil, nil
	}
	var out []ImagePart
	for _, part := range translate.AsArr(content) {
		p := translate.AsMap(part)
		typ := translate.AsStr(p["type"])
		if typ == "image_url" {
			image, err := loadImage(translate.AsStr(translate.AsMap(p["image_url"])["url"]))
			if err != nil {
				return nil, err
			}
			if image != nil {
				out = append(out, *image)
			}
			continue
		}
		if typ == "image" {
			source := translate.AsMap(p["source"])
			media := translate.AsStr(source["media_type"], "image/png")
			data := translate.AsStr(source["data"])
			if data == "" {
				continue
			}
			bytes, err := decodeBase64(data)
			if err != nil {
				return nil, err
			}
			if err := assertSize(bytes); err != nil {
				return nil, err
			}
			out = append(out, ImagePart{Data: bytes, MimeType: media, UUID: randomUUID()})
		}
	}
	return out, nil
}

func loadImage(raw string) (*ImagePart, error) {
	if raw == "" {
		return nil, nil
	}
	if d := parseDataURL(raw); d != nil {
		bytes, err := decodeBase64(d.data)
		if err != nil {
			return nil, err
		}
		if err := assertSize(bytes); err != nil {
			return nil, err
		}
		return &ImagePart{Data: bytes, MimeType: d.media, UUID: randomUUID()}, nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("Image URL is invalid")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return nil, fmt.Errorf("Image URL must use http or https")
	}
	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("Could not fetch image URL (%d)", res.StatusCode)
	}
	bytes, err := io.ReadAll(io.LimitReader(res.Body, MaxImageBytes+1))
	if err != nil {
		return nil, err
	}
	if err := assertSize(bytes); err != nil {
		return nil, err
	}
	mime := res.Header.Get("content-type")
	if i := strings.Index(mime, ";"); i >= 0 {
		mime = mime[:i]
	}
	mime = strings.TrimSpace(mime)
	if mime == "" {
		mime = "image/png"
	}
	return &ImagePart{Data: bytes, MimeType: mime, UUID: randomUUID()}, nil
}

var dataURLRe = regexp.MustCompile(`(?i)^data:([^;,]+);base64,(.+)$`)

type dataURL struct{ media, data string }

func parseDataURL(raw string) *dataURL {
	m := dataURLRe.FindStringSubmatch(raw)
	if m == nil {
		return nil
	}
	return &dataURL{media: m[1], data: m[2]}
}

func decodeBase64(value string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(strings.ReplaceAll(value, " ", ""))
}

func assertSize(b []byte) error {
	if len(b) > MaxImageBytes {
		return fmt.Errorf("Image input is too large. Keep each image under 1MB.")
	}
	if len(b) == 0 {
		return fmt.Errorf("Image input is empty")
	}
	return nil
}

func randomUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
