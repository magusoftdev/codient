package codientcli

import (
	"fmt"
	"os"
	"strings"

	"github.com/openai/openai-go/v3"

	"codient/internal/imageutil"
)

// buildUserMessage combines optional pre-attached images (CLI, /image, or prior loads) with
// the user's text, parses @image:path references, and returns a Chat Completions user message.
func buildUserMessage(workspace string, text string, preAttached []imageutil.ImageAttachment) (openai.ChatCompletionMessageParamUnion, error) {
	text = strings.TrimSpace(text)
	maxB := imageutil.DefaultMaxBytes

	clean, inline, err := imageutil.ParseInlineImages(text, workspace, maxB)
	if err != nil {
		return openai.ChatCompletionMessageParamUnion{}, err
	}

	var imgs []imageutil.ImageAttachment
	imgs = append(imgs, preAttached...)
	imgs = append(imgs, inline...)

	if len(imgs) == 0 {
		return openai.UserMessage(clean), nil
	}

	parts := make([]openai.ChatCompletionContentPartUnionParam, 0, 1+len(imgs))
	if clean != "" {
		parts = append(parts, openai.TextContentPart(clean))
	}
	for _, im := range imgs {
		if im.OrigBytes >= imageutil.WarnLargeBytes {
			fmt.Fprintf(os.Stderr, "codient: warning: large image %q (%d bytes)\n", im.Path, im.OrigBytes)
		}
		parts = append(parts, openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
			URL: im.DataURI,
		}))
	}
	return openai.UserMessage(parts), nil
}

// loadImagePaths loads images from paths for -image / attachments.
func loadImagePaths(paths []string) ([]imageutil.ImageAttachment, error) {
	var out []imageutil.ImageAttachment
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		a, err := imageutil.LoadImage(p, imageutil.DefaultMaxBytes)
		if err != nil {
			return nil, fmt.Errorf("image %q: %w", p, err)
		}
		if a.OrigBytes >= imageutil.WarnLargeBytes {
			fmt.Fprintf(os.Stderr, "codient: warning: large image %q (%d bytes)\n", p, a.OrigBytes)
		}
		out = append(out, a)
	}
	return out, nil
}
