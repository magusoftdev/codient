package agent

import (
	"strings"
	"testing"

	"github.com/openai/openai-go/v3"
)

func TestTruncateHistory_NoLimit(t *testing.T) {
	msgs := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("sys"),
		openai.UserMessage("hello"),
		openai.AssistantMessage("world"),
	}
	out := truncateHistory(msgs, 1, 0, 4096, 0)
	if len(out) != 3 {
		t.Fatalf("expected 3, got %d", len(out))
	}
}

func TestTruncateHistory_TruncatesToolResults(t *testing.T) {
	long := strings.Repeat("x", 8000)
	msgs := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("sys"),
		openai.UserMessage("read file"),
		openai.ToolMessage(long, "call_1"),
		openai.AssistantMessage("got it"),
		openai.UserMessage("now edit"),
		openai.AssistantMessage("sure"),
		openai.UserMessage("latest"),
		openai.AssistantMessage("ok"),
	}
	out := truncateHistory(msgs, 1, 500, 100, 0)
	for _, m := range out {
		text := messageText(m)
		if strings.Contains(text, strings.Repeat("x", 100)) {
			t.Fatal("expected tool result to be truncated")
		}
	}
}

func TestTruncateHistory_DropsOldMessages(t *testing.T) {
	long := strings.Repeat("x", 8000)
	msgs := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("sys"),
		openai.UserMessage(long),
		openai.AssistantMessage(long),
		openai.UserMessage(long),
		openai.AssistantMessage(long),
		openai.UserMessage("recent"),
		openai.AssistantMessage("reply"),
		openai.UserMessage("latest"),
		openai.AssistantMessage("done"),
	}
	out := truncateHistory(msgs, 1, 200, 50, 0)
	if len(out) >= len(msgs) {
		t.Fatalf("expected messages to be dropped, still have %d", len(out))
	}
	// System message should be preserved
	sysText := messageText(out[0])
	if !strings.Contains(sysText, "sys") {
		t.Fatal("system message should be preserved")
	}
}

func TestTruncateHistory_FitsWithinBudget(t *testing.T) {
	msgs := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("sys"),
		openai.UserMessage("hello"),
		openai.AssistantMessage("world"),
	}
	out := truncateHistory(msgs, 1, 10000, 100, 0)
	if len(out) != 3 {
		t.Fatalf("should not truncate when within budget, got %d", len(out))
	}
}

func TestTruncateHistory_ToolsOverheadReducesBudget(t *testing.T) {
	long := strings.Repeat("x", 4000)
	msgs := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("sys"),
		openai.UserMessage(long),
		openai.AssistantMessage(long),
		openai.UserMessage("recent"),
		openai.AssistantMessage("reply"),
		openai.UserMessage("latest"),
		openai.AssistantMessage("done"),
	}
	// Without overhead: fits in budget.
	out1 := truncateHistory(append([]openai.ChatCompletionMessageParamUnion(nil), msgs...), 1, 5000, 100, 0)
	// With large tool overhead: should trigger truncation.
	out2 := truncateHistory(append([]openai.ChatCompletionMessageParamUnion(nil), msgs...), 1, 5000, 100, 3000)
	if len(out2) >= len(out1) {
		t.Fatalf("tools overhead should force more aggressive truncation: without=%d with=%d", len(out1), len(out2))
	}
}
