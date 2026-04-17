package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"codient/internal/repomap"

	"github.com/openai/openai-go/v3/shared"
)

const maxRepoMapToolTokens = 32000

func registerRepoMap(r *Registry, m *repomap.Map) {
	if m == nil {
		return
	}

	r.Register(Tool{
		Name: "repo_map",
		Description: "Returns a structural map of the workspace: source files and extracted top-level symbols " +
			"(functions, types, classes, etc.). Use for a bird's-eye view of layout and where to look next. " +
			"Optional path_prefix scopes to a subdirectory; optional max_tokens limits output size.",
		Parameters: shared.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"path_prefix": map[string]any{
					"type":        "string",
					"description": "Optional subdirectory relative to workspace (e.g. \"internal/agent\"). Empty means entire workspace.",
				},
				"max_tokens": map[string]any{
					"type":        "integer",
					"description": "Approximate maximum size of the map in tokens (default: auto from workspace size, capped).",
				},
			},
			"additionalProperties": false,
		},
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var p struct {
				PathPrefix string `json:"path_prefix"`
				MaxTokens  *int   `json:"max_tokens"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}

			select {
			case <-m.Ready():
			case <-ctx.Done():
				return "", ctx.Err()
			}
			if err := m.BuildErr(); err != nil {
				return "", fmt.Errorf("repo map: %w", err)
			}

			tok := repomap.AutoTokens(m.FileCount())
			if p.MaxTokens != nil && *p.MaxTokens > 0 {
				tok = *p.MaxTokens
			}
			if tok > maxRepoMapToolTokens {
				tok = maxRepoMapToolTokens
			}

			prefix := strings.TrimSpace(p.PathPrefix)
			out := m.RenderPrefix(prefix, tok)
			if strings.TrimSpace(out) == "" {
				return "No symbols in the repo map for this scope (empty index, or path_prefix matches no files).", nil
			}
			return out, nil
		},
	})
}
