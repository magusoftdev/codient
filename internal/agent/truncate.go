package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"

	"codient/internal/tokenest"
)

// truncateHistory trims messages so their estimated token count fits within
// contextLimit - reserveTokens - toolsOverhead. toolsOverhead accounts for tool
// definition JSON that the server counts but is not part of the message slice.
// When contextLimit is 0 the input is returned unchanged (no-limit mode).
func truncateHistory(msgs []openai.ChatCompletionMessageParamUnion, sysOffset int, contextLimit, reserveTokens, toolsOverhead int) []openai.ChatCompletionMessageParamUnion {
	if contextLimit <= 0 {
		return msgs
	}
	budget := contextLimit - reserveTokens - toolsOverhead
	if budget < 1 {
		budget = 1
	}

	if estimateSlice(msgs) <= budget {
		return msgs
	}

	// Phase 1: replace tool-result messages (oldest first) with a short stub.
	// Skip system (index < sysOffset) and the most recent 4 messages.
	keep := 4
	if keep > len(msgs)-sysOffset {
		keep = len(msgs) - sysOffset
	}
	cutoff := len(msgs) - keep
	for i := sysOffset; i < cutoff; i++ {
		if isToolMessage(msgs[i]) {
			orig := messageText(msgs[i])
			stub := fmt.Sprintf("[truncated tool result — was %d bytes]", len(orig))
			msgs[i] = replaceToolContent(msgs[i], stub)
		}
		if estimateSlice(msgs) <= budget {
			return msgs
		}
	}

	// Phase 2: drop oldest non-system messages until we fit.
	// Keep system + most recent 2 exchanges (4 messages minimum).
	minKeep := sysOffset + 4
	if minKeep > len(msgs) {
		minKeep = len(msgs)
	}
	for len(msgs) > minKeep && estimateSlice(msgs) > budget {
		drop := -1
		for i := sysOffset; i < len(msgs) && len(msgs) > minKeep; i++ {
			if isSessionSummaryMessage(msgs[i]) {
				continue
			}
			drop = i
			break
		}
		if drop < 0 {
			break
		}
		msgs = append(msgs[:drop], msgs[drop+1:]...)
	}
	return msgs
}

func estimateSlice(msgs []openai.ChatCompletionMessageParamUnion) int {
	total := 0
	for _, m := range msgs {
		total += tokenest.Estimate(messageText(m)) + 4
	}
	return total
}

func messageText(m openai.ChatCompletionMessageParamUnion) string {
	b, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(b)
}

func isToolMessage(m openai.ChatCompletionMessageParamUnion) bool {
	return m.OfTool != nil
}

func replaceToolContent(m openai.ChatCompletionMessageParamUnion, content string) openai.ChatCompletionMessageParamUnion {
	if m.OfTool != nil {
		return openai.ToolMessage(content, m.OfTool.ToolCallID)
	}
	return m
}

func isSessionSummaryMessage(m openai.ChatCompletionMessageParamUnion) bool {
	return m.OfAssistant != nil && strings.Contains(messageText(m), "[Session summary]")
}
