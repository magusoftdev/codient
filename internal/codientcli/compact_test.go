package codientcli

import (
	"testing"

	"github.com/openai/openai-go/v3"

	"codient/internal/tokenest"
)

func TestEstimateHistoryTokens_Empty(t *testing.T) {
	got := estimateHistoryTokens(nil)
	if got != 0 {
		t.Fatalf("estimateHistoryTokens(nil) = %d, want 0", got)
	}
}

func TestEstimateHistoryTokens_SingleMessage(t *testing.T) {
	history := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("hello world"),
	}
	got := estimateHistoryTokens(history)
	if got < 1 {
		t.Fatalf("estimateHistoryTokens should be positive, got %d", got)
	}
}

func TestEstimateHistoryTokens_MultipleMessages(t *testing.T) {
	history := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("hello world"),
		openai.AssistantMessage("I can help with that."),
	}
	single := estimateHistoryTokens(history[:1])
	both := estimateHistoryTokens(history)
	if both <= single {
		t.Fatalf("two messages (%d) should estimate higher than one (%d)", both, single)
	}
}

func TestEstimateHistoryTokens_IncludesOverhead(t *testing.T) {
	history := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("x"),
	}
	got := estimateHistoryTokens(history)
	bare := tokenest.Estimate(messageTextForEstimate(history[0]))
	if got != bare+tokenest.MessageOverhead {
		t.Fatalf("estimateHistoryTokens = %d, want %d (bare) + %d (overhead)", got, bare, tokenest.MessageOverhead)
	}
}

func TestMinHistoryForCompact(t *testing.T) {
	if minHistoryForCompact < 2 {
		t.Fatalf("minHistoryForCompact = %d, should be at least 2", minHistoryForCompact)
	}
}
