package agent

import (
	"testing"

	"github.com/openai/openai-go/v3"
)

func TestUserMessageText_plainString(t *testing.T) {
	m := openai.UserMessage("hello")
	if g := UserMessageText(m); g != "hello" {
		t.Fatalf("got %q", g)
	}
}

func TestUserMessageText_multipart(t *testing.T) {
	parts := []openai.ChatCompletionContentPartUnionParam{
		openai.TextContentPart("see screenshot"),
		openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{URL: "data:image/png;base64,abc"}),
	}
	m := openai.UserMessage(parts)
	g := UserMessageText(m)
	if g != "see screenshot [image]" {
		t.Fatalf("got %q", g)
	}
}
