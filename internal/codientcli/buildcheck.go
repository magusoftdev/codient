package codientcli

import (
	"context"

	"codient/internal/agent"
)

const buildSelfCritiquePrompt = `Before finalizing, do one concise self-critique pass on the changes you just made.

Find up to three plausible reasons the change might be wrong. Verify with focused reads, greps, or checks when useful. Fix any real issue you find. If nothing needs fixing, reply with the final answer. Do not repeat this self-critique request.`

func makeBuildSelfCritique() func(context.Context, agent.PostReplyCheckInfo) string {
	return func(_ context.Context, info agent.PostReplyCheckInfo) string {
		if !info.Mutated || info.AutoCheckExhausted {
			return ""
		}
		for _, name := range info.TurnTools {
			if agent.ToolIsMutating(name) {
				return buildSelfCritiquePrompt
			}
		}
		return ""
	}
}
