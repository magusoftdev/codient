package codientcli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"

	"codient/internal/sessionstore"
	"codient/internal/tokenest"
	"codient/internal/tokentracker"
)

// compactHistory uses the LLM to summarize the conversation history, replacing
// all messages with a single summary message to free context space.
func (s *session) compactHistory(ctx context.Context) error {
	if len(s.history) == 0 {
		fmt.Fprintf(os.Stderr, "codient: nothing to compact\n")
		return nil
	}

	beforeTokens := estimateHistoryTokens(s.history)

	// Build the text to summarize from user + assistant messages.
	var sb strings.Builder
	for _, raw := range sessionstore.FromOpenAI(s.history) {
		role := sessionstore.MessageRole(raw)
		content := sessionstore.MessageContent(raw)
		if (role == "user" || role == "assistant") && content != "" {
			sb.WriteString(role)
			sb.WriteString(": ")
			sb.WriteString(content)
			sb.WriteString("\n\n")
		}
	}
	conversation := strings.TrimSpace(sb.String())
	if conversation == "" {
		fmt.Fprintf(os.Stderr, "codient: no text content to summarize\n")
		return nil
	}

	fmt.Fprintf(os.Stderr, "codient: compacting history (~%d tokens)...\n", beforeTokens)

	systemMsg := "You are a helpful assistant. Summarize the following conversation concisely in 2-3 paragraphs. " +
		"Preserve: the original user goal, key decisions made, specific file paths and function names mentioned, action items, " +
		"active assumptions or constraints, completed plan steps, skipped steps and reasons, and verification/build/test state. " +
		"Do not add new information."

	msgs := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(systemMsg),
		openai.UserMessage("Summarize this conversation:\n\n" + conversation),
	}
	params := openai.ChatCompletionNewParams{
		Model:    shared.ChatModel(s.client.Model()),
		Messages: msgs,
	}

	res, err := s.client.ChatCompletion(ctx, params)
	if err != nil {
		return fmt.Errorf("compact LLM call: %w", err)
	}
	if s.tokenTracker != nil {
		s.tokenTracker.Add(tokentracker.Usage{
			PromptTokens:     res.Usage.PromptTokens,
			CompletionTokens: res.Usage.CompletionTokens,
			TotalTokens:      res.Usage.TotalTokens,
		})
	}
	if len(res.Choices) == 0 || res.Choices[0].Message.Content == "" {
		return fmt.Errorf("compact: empty response from model")
	}

	summary := res.Choices[0].Message.Content

	// Replace history with the summary as an assistant message.
	s.history = []openai.ChatCompletionMessageParamUnion{
		openai.AssistantMessage("[Session summary]\n\n" + summary),
	}
	s.lastReply = ""

	afterTokens := estimateHistoryTokens(s.history)
	fmt.Fprintf(os.Stderr, "codient: compacted ~%d tokens -> ~%d tokens (saved ~%d)\n",
		beforeTokens, afterTokens, beforeTokens-afterTokens)

	s.autoSave()
	return nil
}

func estimateHistoryTokens(history []openai.ChatCompletionMessageParamUnion) int {
	total := 0
	for _, m := range history {
		total += tokenest.Estimate(messageTextForEstimate(m)) + tokenest.MessageOverhead
	}
	return total
}

// estimateFullContextUsage returns the estimated total tokens for the next API request:
// system prompt + tool definitions + history messages.
func (s *session) estimateFullContextUsage() int {
	s.promptMu.RLock()
	defer s.promptMu.RUnlock()
	sys := tokenest.Estimate(s.systemPrompt) + tokenest.MessageOverhead
	toolJSON, _ := json.Marshal(s.registry.OpenAITools())
	toolsTok := tokenest.Estimate(string(toolJSON))
	hist := estimateHistoryTokens(s.history)
	return sys + toolsTok + hist
}

// minHistoryForCompact is the minimum number of history messages required
// before auto-compaction is considered (avoids compacting trivially short sessions).
const minHistoryForCompact = 4

// maybeAutoCompact checks context pressure after a turn and automatically
// compacts history if usage exceeds the configured threshold.
func (s *session) maybeAutoCompact(ctx context.Context) {
	if s.cfg.ContextWindowTokens <= 0 || s.cfg.AutoCompactPct <= 0 {
		return
	}
	if len(s.history) < minHistoryForCompact {
		return
	}
	usage := s.estimateFullContextUsage()
	threshold := s.cfg.ContextWindowTokens * s.cfg.AutoCompactPct / 100
	if usage <= threshold {
		return
	}
	pct := usage * 100 / s.cfg.ContextWindowTokens
	fmt.Fprintf(os.Stderr, "codient: context ~%d%% full (~%d / %d tokens) — auto-compacting...\n",
		pct, usage, s.cfg.ContextWindowTokens)
	if err := s.compactHistory(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "codient: auto-compact failed: %v\n", err)
	}
}
