// Package imageutil loads and validates image files for multimodal chat completions.
package imageutil

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"  // decode GIF config
	_ "image/jpeg" // decode JPEG config
	_ "image/png"  // decode PNG config
	"os"
	"path/filepath"
	"regexp"
	"strings"

	_ "golang.org/x/image/webp" // decode WebP config
)

// DefaultMaxBytes is the default maximum raw image file size (20 MiB).
const DefaultMaxBytes = 20 << 20

// WarnLargeBytes triggers a stderr warning when exceeded (5 MiB).
const WarnLargeBytes = 5 << 20

// ImageAttachment holds a loaded image as a data URI for the Chat Completions API.
type ImageAttachment struct {
	Path      string
	MimeType  string
	DataURI   string
	OrigBytes int
}

var imageExtOK = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
}

// SupportedImageExt reports whether the path has a known image extension.
func SupportedImageExt(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	_, ok := imageExtOK[ext]
	return ok
}

// MimeForExt returns the MIME type for a path extension, or empty if unknown.
func MimeForExt(path string) string {
	return imageExtOK[strings.ToLower(filepath.Ext(path))]
}

// LoadImage reads path, validates it as an image, and returns a data URI suitable
// for openai.ImageContentPart (url field).
func LoadImage(path string, maxBytes int) (ImageAttachment, error) {
	var zero ImageAttachment
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	path = filepath.Clean(path)
	b, err := os.ReadFile(path)
	if err != nil {
		return zero, fmt.Errorf("read image: %w", err)
	}
	if len(b) > maxBytes {
		return zero, fmt.Errorf("image %q exceeds max size %d bytes", path, maxBytes)
	}
	mime := MimeForExt(path)
	if mime == "" {
		return zero, fmt.Errorf("unsupported image extension for %q (use png, jpg, jpeg, gif, webp)", path)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return zero, fmt.Errorf("decode image config %q: %w", path, err)
	}
	_ = format // we trust extension for MIME; config validates bytes
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return zero, fmt.Errorf("invalid image dimensions in %q", path)
	}
	enc := base64.StdEncoding.EncodeToString(b)
	dataURI := fmt.Sprintf("data:%s;base64,%s", mime, enc)
	return ImageAttachment{
		Path:      path,
		MimeType:  mime,
		DataURI:   dataURI,
		OrigBytes: len(b),
	}, nil
}

// inlinePattern matches @image:path where path is quoted or unquoted (no spaces).
var inlinePattern = regexp.MustCompile(`@image:(?:"([^"]+)"|'([^']+)'|(\S+))`)

// ParseInlineImages finds @image:... references in text, loads each image from baseDir,
// and returns the text with those references removed (trimmed). Relative paths resolve under baseDir.
func ParseInlineImages(text string, baseDir string, maxBytes int) (clean string, attachments []ImageAttachment, err error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	baseDir = strings.TrimSpace(baseDir)
	var out strings.Builder
	last := 0
	for _, sm := range inlinePattern.FindAllStringSubmatchIndex(text, -1) {
		// sm: [full_start, full_end, g1_start, g1_end, g2_start, g2_end, g3_start, g3_end]
		if len(sm) < 2 {
			continue
		}
		out.WriteString(text[last:sm[0]])
		last = sm[1]

		var rawPath string
		if len(sm) >= 4 && sm[2] >= 0 && sm[3] > sm[2] {
			rawPath = text[sm[2]:sm[3]]
		} else if len(sm) >= 6 && sm[4] >= 0 && sm[5] > sm[4] {
			rawPath = text[sm[4]:sm[5]]
		} else if len(sm) >= 8 && sm[6] >= 0 && sm[7] > sm[6] {
			rawPath = text[sm[6]:sm[7]]
		}
		rawPath = strings.TrimSpace(rawPath)
		if rawPath == "" {
			continue
		}
		p := rawPath
		if !filepath.IsAbs(p) && baseDir != "" {
			p = filepath.Join(baseDir, p)
		}
		p = filepath.Clean(p)
		if !SupportedImageExt(p) {
			err = fmt.Errorf("@image: unsupported extension for %q", rawPath)
			return "", nil, err
		}
		img, loadErr := LoadImage(p, maxBytes)
		if loadErr != nil {
			err = fmt.Errorf("@image %q: %w", rawPath, loadErr)
			return "", nil, err
		}
		attachments = append(attachments, img)
	}
	out.WriteString(text[last:])
	clean = strings.TrimSpace(out.String())
	return clean, attachments, nil
}

// EstimateImageTokens approximates vision input tokens for budgeting.
// Uses OpenAI-style tiling: 85 base + 170 tokens per 512×512 tile (detail auto/high heuristic).
// Falls back to a size-based estimate when dimensions cannot be read from the data URI.
func EstimateImageTokens(dataURI string) int {
	if dataURI == "" {
		return 0
	}
	const prefix = "data:"
	if !strings.HasPrefix(dataURI, prefix) {
		return 170 // minimal non-zero
	}
	rest := dataURI[len(prefix):]
	semi := strings.Index(rest, ";base64,")
	if semi < 0 {
		return 170
	}
	b64 := rest[semi+len(";base64,"):]
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return estimateTokensFromLen(len(dataURI))
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return estimateTokensFromLen(len(raw))
	}
	w, h := cfg.Width, cfg.Height
	if w <= 0 || h <= 0 {
		return estimateTokensFromLen(len(raw))
	}
	// Tiles: ceil(w/512) * ceil(h/512), cap similar to OpenAI vision docs
	tw := (w + 511) / 512
	th := (h + 511) / 512
	tiles := tw * th
	return 85 + 170*tiles
}

func estimateTokensFromLen(n int) int {
	if n <= 0 {
		return 85
	}
	// Rough: ~1 token per 3–4 bytes of base64 payload for images (very approximate)
	return 85 + n/400
}
