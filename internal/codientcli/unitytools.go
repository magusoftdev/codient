package codientcli

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/openai/openai-go/v3/shared"

	"codient/internal/prompt"
	"codient/internal/tools"
)

const unityACPDefaultTimeout = 90 * time.Second

// unityACPToolCtx returns a context bounded for a single Unity JSON-RPC call.
func unityACPToolCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, unityACPDefaultTimeout)
}

func unityCall(call func(context.Context, string, any) (json.RawMessage, error), ctx context.Context, method string, params any) (string, error) {
	cctx, cancel := unityACPToolCtx(ctx)
	defer cancel()
	raw, err := call(cctx, method, params)
	if err != nil {
		return "", err
	}
	if len(raw) == 0 {
		return "{}", nil
	}
	return string(raw), nil
}

func paramsObject(args json.RawMessage) (map[string]any, error) {
	if len(args) == 0 {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := json.Unmarshal(args, &m); err != nil {
		return nil, fmt.Errorf("invalid arguments JSON: %w", err)
	}
	if m == nil {
		return map[string]any{}, nil
	}
	return m, nil
}

// registerUnityACPReadTools registers Unity Editor read-only tools (ACP client JSON-RPC).
func registerUnityACPReadTools(reg *tools.Registry, call func(context.Context, string, any) (json.RawMessage, error)) {
	reg.Register(tools.Tool{
		Name: "unity_query_scene_hierarchy",
		Description: "Query the Unity Editor scene hierarchy (loaded scenes). " +
			"Uses EntityId (ulong) from this tool for game objects. " +
			"Optional: scenePath (project-relative, e.g. Assets/Scenes/Main.unity), rootGameObjectEntityId, maxDepth (0 = roots only). " +
			"Omit scenePath to use the active scene.",
		Parameters: shared.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"scenePath": map[string]any{
					"type":        "string",
					"description": "Optional .unity asset path under the project; must already be loaded.",
				},
				"rootGameObjectEntityId": map[string]any{
					"type":        "integer",
					"description": "Optional ulong root GameObject EntityId (JSON number).",
				},
				"rootGameObjectInstanceId": map[string]any{
					"type":        "integer",
					"description": "Optional legacy Unity instance id (int) when EntityId is unknown.",
				},
				"maxDepth": map[string]any{
					"type":        "integer",
					"description": "Max hierarchy depth (default 64; 0 = roots only).",
				},
			},
			"additionalProperties": false,
		},
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			p, err := paramsObject(args)
			if err != nil {
				return "", err
			}
			return unityCall(call, ctx, "unity/query_scene_hierarchy", p)
		},
	})

	reg.Register(tools.Tool{
		Name: "unity_search_asset_database",
		Description: "Search the Unity AssetDatabase (Editor). " +
			"searchFilter is the same filter string passed to AssetDatabase.FindAssets (type labels, names, etc.). " +
			"Optional searchInFolders: relative project paths to scope the search.",
		Parameters: shared.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"searchFilter": map[string]any{
					"type":        "string",
					"description": "AssetDatabase search filter (required).",
				},
				"searchInFolders": map[string]any{
					"type":        "array",
					"description": "Optional folder paths relative to project root.",
					"items":       map[string]any{"type": "string"},
				},
			},
			"required":             []string{"searchFilter"},
			"additionalProperties": false,
		},
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			p, err := paramsObject(args)
			if err != nil {
				return "", err
			}
			return unityCall(call, ctx, "unity/search_asset_database", p)
		},
	})

	reg.Register(tools.Tool{
		Name: "unity_inspect_component",
		Description: "Read visible serialized fields for a component on a GameObject via the Unity Editor. " +
			"Provide gameObjectEntityId (ulong) or legacy gameObjectInstanceId (int) plus componentTypeName (short or full type name).",
		Parameters: shared.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"gameObjectEntityId": map[string]any{
					"type":        "integer",
					"description": "GameObject EntityId as JSON number (ulong).",
				},
				"gameObjectInstanceId": map[string]any{
					"type":        "integer",
					"description": "Legacy GameObject instance id (int).",
				},
				"componentTypeName": map[string]any{
					"type":        "string",
					"description": "Component type name (e.g. Transform or full name).",
				},
			},
			"required":             []string{"componentTypeName"},
			"additionalProperties": false,
		},
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			p, err := paramsObject(args)
			if err != nil {
				return "", err
			}
			return unityCall(call, ctx, "unity/inspect_component", p)
		},
	})

	reg.Register(tools.Tool{
		Name:        "unity_list_loaded_scenes",
		Description: "List scenes currently loaded in the Unity Editor (name, path, build index, dirty flag, active).",
		Parameters: shared.FunctionParameters{
			"type":                 "object",
			"properties":           map[string]any{},
			"additionalProperties": false,
		},
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			_, err := paramsObject(args)
			if err != nil {
				return "", err
			}
			return unityCall(call, ctx, "unity/list_loaded_scenes", map[string]any{})
		},
	})

	reg.Register(tools.Tool{
		Name: "unity_query_prefab_hierarchy",
		Description: "Load a prefab asset temporarily and return its root hierarchy (same shape as scene hierarchy). " +
			"prefabAssetPath must be a project-relative path to a .prefab file.",
		Parameters: shared.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"prefabAssetPath": map[string]any{
					"type":        "string",
					"description": "Path like Assets/Prefabs/Foo.prefab",
				},
				"maxDepth": map[string]any{
					"type":        "integer",
					"description": "Max hierarchy depth (default 64).",
				},
			},
			"required":             []string{"prefabAssetPath"},
			"additionalProperties": false,
		},
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			p, err := paramsObject(args)
			if err != nil {
				return "", err
			}
			return unityCall(call, ctx, "unity/query_prefab_hierarchy", p)
		},
	})

	reg.Register(tools.Tool{
		Name: "unity_get_console_errors",
		Description: "Return recent Unity Editor console messages (errors and warnings, newest last). " +
			"Optional maxEntries (default 50, max 200).",
		Parameters: shared.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"maxEntries": map[string]any{
					"type":        "integer",
					"description": "Maximum log lines to return.",
				},
			},
			"additionalProperties": false,
		},
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			p, err := paramsObject(args)
			if err != nil {
				return "", err
			}
			return unityCall(call, ctx, "unity/get_console_errors", p)
		},
	})

	reg.Register(tools.Tool{
		Name:        "unity_summarize_project_packages",
		Description: "Summarize Packages/manifest.json dependencies and list .asmdef files under Assets (bounded).",
		Parameters: shared.FunctionParameters{
			"type":                 "object",
			"properties":           map[string]any{},
			"additionalProperties": false,
		},
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			_, err := paramsObject(args)
			if err != nil {
				return "", err
			}
			return unityCall(call, ctx, "unity/summarize_project_packages", map[string]any{})
		},
	})
}

// registerUnityACPApplyTool registers the in-editor mutation tool (build mode only).
func registerUnityACPApplyTool(reg *tools.Registry, call func(context.Context, string, any) (json.RawMessage, error)) {
	reg.Register(tools.Tool{
		Name: "unity_apply_actions",
		Description: "Apply a batch of structured Unity Editor actions (GameObjects, components, serialized fields). " +
			"schemaVersion must be 1. The Unity Editor shows a confirmation dialog before mutating the project. " +
			"Use unity_query_scene_hierarchy / unity_inspect_component first to obtain EntityIds. " +
			"Operations: create_empty_gameobject, destroy_gameobject, set_gameobject_name, set_parent, add_component, set_component_property.",
		Parameters: shared.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"schemaVersion": map[string]any{
					"type":        "integer",
					"description": "Must be 1.",
				},
				"actions": map[string]any{
					"type":        "array",
					"description": "Ordered list of action objects with field op.",
					"items": map[string]any{
						"type":                 "object",
						"additionalProperties": true,
					},
				},
			},
			"required":             []string{"schemaVersion", "actions"},
			"additionalProperties": false,
		},
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			p, err := paramsObject(args)
			if err != nil {
				return "", err
			}
			return unityCall(call, ctx, "unity/apply_actions", p)
		},
	})
}

// registerUnityACPToolsForMode registers Unity ACP tools when the session is wired to an ACP client.
func registerUnityACPToolsForMode(reg *tools.Registry, mode prompt.Mode, call func(context.Context, string, any) (json.RawMessage, error)) {
	if call == nil {
		return
	}
	registerUnityACPReadTools(reg, call)
	if mode == prompt.ModeBuild {
		registerUnityACPApplyTool(reg, call)
	}
}
