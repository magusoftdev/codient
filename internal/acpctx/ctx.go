// Package acpctx carries per-request metadata on context for ACP-adjacent tool paths.
package acpctx

import "context"

type toolCallIDKey struct{}

// WithToolCallID returns ctx carrying the OpenAI tool call id (or fallback id) for this tool invocation.
func WithToolCallID(ctx context.Context, toolCallID string) context.Context {
	return context.WithValue(ctx, toolCallIDKey{}, toolCallID)
}

// ToolCallID returns the tool call id from ctx, or empty string if unset.
func ToolCallID(ctx context.Context) string {
	s, _ := ctx.Value(toolCallIDKey{}).(string)
	return s
}
