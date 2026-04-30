package agent

import (
	"encoding/json"
	"regexp"
	"strings"
)

// textToolCall represents a tool invocation parsed from XML-style markup
// in the model's text content (e.g. Qwen3-coder format).
type textToolCall struct {
	Name string
	Args map[string]string
}

var (
	// Matches <function=NAME>...</function> blocks.
	reFunctionBlock = regexp.MustCompile(`(?s)<function=([^>]+)>(.*?)</function>`)
	// Matches <parameter=KEY>VALUE</parameter> inside a function block.
	reParameter = regexp.MustCompile(`(?s)<parameter=([^>]+)>(.*?)</parameter>`)
	// Matches <tool_call>JSON</tool_call> blocks (Qwen-style).
	reToolCallJSON = regexp.MustCompile(`(?s)<tool_call>\s*(\{.*?\})\s*</tool_call>`)
)

// containsTextToolCalls is a fast check for whether content likely contains
// XML-style tool calls worth parsing.
func containsTextToolCalls(content string) bool {
	return strings.Contains(content, "<function=") || strings.Contains(content, "<tool_call>")
}

// parseTextToolCalls extracts XML-style tool calls from text content.
// Supports both <function=NAME><parameter=K>V</parameter></function>
// and <tool_call>{"name":"...","arguments":{...}}</tool_call> formats.
// Returns nil if no valid tool calls are found.
func parseTextToolCalls(content string) []textToolCall {
	var calls []textToolCall

	// Try <function=NAME> format first.
	funcMatches := reFunctionBlock.FindAllStringSubmatch(content, -1)
	for _, m := range funcMatches {
		name := strings.TrimSpace(m[1])
		body := m[2]
		if name == "" {
			continue
		}

		args := make(map[string]string)
		params := reParameter.FindAllStringSubmatch(body, -1)
		for _, p := range params {
			key := strings.TrimSpace(p[1])
			val := strings.TrimSpace(p[2])
			if key != "" {
				args[key] = val
			}
		}

		calls = append(calls, textToolCall{Name: name, Args: args})
	}

	// Try <tool_call>JSON</tool_call> format (Qwen-style).
	tcMatches := reToolCallJSON.FindAllStringSubmatch(content, -1)
	for _, m := range tcMatches {
		var payload struct {
			Name      string                     `json:"name"`
			Arguments map[string]json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal([]byte(m[1]), &payload); err != nil || payload.Name == "" {
			continue
		}
		args := make(map[string]string, len(payload.Arguments))
		for k, v := range payload.Arguments {
			s := string(v)
			if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
				var unquoted string
				if err := json.Unmarshal(v, &unquoted); err == nil {
					s = unquoted
				}
			}
			args[k] = s
		}
		calls = append(calls, textToolCall{Name: payload.Name, Args: args})
	}

	if len(calls) == 0 {
		return nil
	}
	return calls
}

// textToolCallArgsJSON converts the string map to a JSON RawMessage
// suitable for Registry.Run. Values that are valid JSON (arrays, objects,
// numbers, booleans, null) are embedded as-is; everything else is quoted
// as a string.
func textToolCallArgsJSON(args map[string]string) json.RawMessage {
	if len(args) == 0 {
		return json.RawMessage(`{}`)
	}
	m := make(map[string]json.RawMessage, len(args))
	for k, v := range args {
		if looksLikeRawJSON(v) && json.Valid([]byte(v)) {
			m[k] = json.RawMessage(v)
		} else {
			quoted, _ := json.Marshal(v)
			m[k] = json.RawMessage(quoted)
		}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}

// reToolCallMarkup matches XML tool call tags and complete blocks that may
// appear as fragments in a text reply (e.g. when the model hits the step
// limit mid-call). Handles both <function=...>...</function> and
// <tool_call>JSON</tool_call> formats.
var reToolCallMarkup = regexp.MustCompile(`(?s)<tool_call>\s*\{.*?\}\s*</tool_call>|</?(?:tool_call|function(?:=[^>]*)?)>|<parameter=[^>]*>.*?</parameter>`)

// stripTextToolCallFragments removes XML tool call markup from text content.
// Used to clean up the final reply when the model emits partial/leftover
// tool call XML that wasn't parsed as a complete invocation.
func stripTextToolCallFragments(content string) string {
	cleaned := reToolCallMarkup.ReplaceAllString(content, "")
	return strings.TrimSpace(cleaned)
}

// looksLikeRawJSON returns true if the value starts with a character that
// could indicate a non-string JSON value (array, object, number, bool, null).
func looksLikeRawJSON(v string) bool {
	if v == "" {
		return false
	}
	switch v[0] {
	case '[', '{':
		return true
	case 't':
		return v == "true"
	case 'f':
		return v == "false"
	case 'n':
		return v == "null"
	}
	if v[0] == '-' || (v[0] >= '0' && v[0] <= '9') {
		return true
	}
	return false
}
