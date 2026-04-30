package agent

import (
	"encoding/json"
	"testing"
)

func TestParseTextToolCalls_Single(t *testing.T) {
	content := `Let me check the workspace:
<tool_call>
<function=list_dir>
<parameter=path>.</parameter>
</function>
</tool_call>`

	calls := parseTextToolCalls(content)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "list_dir" {
		t.Fatalf("name: got %q", calls[0].Name)
	}
	if calls[0].Args["path"] != "." {
		t.Fatalf("path arg: got %q", calls[0].Args["path"])
	}
}

func TestParseTextToolCalls_Multiple(t *testing.T) {
	content := `I'll read two files:
<tool_call>
<function=read_file>
<parameter=path>main.go</parameter>
</function>
</tool_call>
<tool_call>
<function=read_file>
<parameter=path>go.mod</parameter>
</function>
</tool_call>`

	calls := parseTextToolCalls(content)
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[0].Args["path"] != "main.go" {
		t.Fatalf("first path: got %q", calls[0].Args["path"])
	}
	if calls[1].Args["path"] != "go.mod" {
		t.Fatalf("second path: got %q", calls[1].Args["path"])
	}
}

func TestParseTextToolCalls_MultipleParams(t *testing.T) {
	content := `<function=str_replace>
<parameter=path>main.go</parameter>
<parameter=old_str>fmt.Println("hello")</parameter>
<parameter=new_str>fmt.Println("world")</parameter>
</function>`

	calls := parseTextToolCalls(content)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "str_replace" {
		t.Fatalf("name: got %q", calls[0].Name)
	}
	if calls[0].Args["path"] != "main.go" {
		t.Fatalf("path: got %q", calls[0].Args["path"])
	}
	if calls[0].Args["old_str"] != `fmt.Println("hello")` {
		t.Fatalf("old_str: got %q", calls[0].Args["old_str"])
	}
	if calls[0].Args["new_str"] != `fmt.Println("world")` {
		t.Fatalf("new_str: got %q", calls[0].Args["new_str"])
	}
}

func TestParseTextToolCalls_NoWrapper(t *testing.T) {
	content := `<function=list_dir>
<parameter=path>/src</parameter>
</function>`

	calls := parseTextToolCalls(content)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "list_dir" {
		t.Fatalf("name: got %q", calls[0].Name)
	}
}

func TestParseTextToolCalls_None(t *testing.T) {
	calls := parseTextToolCalls("This is just a regular text reply with no tool calls.")
	if len(calls) != 0 {
		t.Fatalf("expected 0 calls, got %d", len(calls))
	}
}

func TestParseTextToolCalls_Malformed(t *testing.T) {
	cases := []string{
		"<function=>missing name</function>",
		"<function=foo>no closing tag",
		"just <parameter=x>orphan</parameter> params",
	}
	for _, c := range cases {
		calls := parseTextToolCalls(c)
		for _, call := range calls {
			if call.Name == "" {
				t.Fatalf("should not produce calls with empty name for input %q", c)
			}
		}
	}
}

func TestParseTextToolCalls_QwenJSONSingle(t *testing.T) {
	content := `<tool_call>{"name":"read_file","arguments":{"path":"main.go"}}</tool_call>`
	calls := parseTextToolCalls(content)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "read_file" {
		t.Fatalf("name: got %q", calls[0].Name)
	}
	if calls[0].Args["path"] != "main.go" {
		t.Fatalf("path arg: got %q", calls[0].Args["path"])
	}
}

func TestParseTextToolCalls_QwenJSONMultiple(t *testing.T) {
	content := `<tool_call>{"name":"read_file","arguments":{"path":"main.go"}}</tool_call>
<tool_call>{"name":"list_dir","arguments":{"path":"."}}</tool_call>`
	calls := parseTextToolCalls(content)
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[0].Name != "read_file" {
		t.Fatalf("first name: got %q", calls[0].Name)
	}
	if calls[1].Name != "list_dir" {
		t.Fatalf("second name: got %q", calls[1].Name)
	}
}

func TestParseTextToolCalls_QwenJSONBadJSON(t *testing.T) {
	content := `<tool_call>not valid json</tool_call>`
	calls := parseTextToolCalls(content)
	if len(calls) != 0 {
		t.Fatalf("expected 0 calls for malformed JSON, got %d", len(calls))
	}
}

func TestParseTextToolCalls_QwenJSONNumericArg(t *testing.T) {
	content := `<tool_call>{"name":"read_file","arguments":{"path":"main.go","max_lines":100}}</tool_call>`
	calls := parseTextToolCalls(content)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Args["path"] != "main.go" {
		t.Fatalf("path: got %q", calls[0].Args["path"])
	}
	if calls[0].Args["max_lines"] != "100" {
		t.Fatalf("max_lines: got %q", calls[0].Args["max_lines"])
	}
}

func TestContainsTextToolCalls(t *testing.T) {
	if !containsTextToolCalls("<function=list_dir>") {
		t.Fatal("expected true for <function=")
	}
	if !containsTextToolCalls(`<tool_call>{"name":"read_file"}</tool_call>`) {
		t.Fatal("expected true for <tool_call>")
	}
	if containsTextToolCalls("no tool calls here") {
		t.Fatal("expected false")
	}
}

func TestTextToolCallArgsJSON_Strings(t *testing.T) {
	args := map[string]string{"path": ".", "content": "hello world"}
	raw := textToolCallArgsJSON(args)
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m["path"] != "." || m["content"] != "hello world" {
		t.Fatalf("unexpected: %v", m)
	}
}

func TestTextToolCallArgsJSON_PreservesArray(t *testing.T) {
	args := map[string]string{"argv": `["go", "version"]`, "cwd": "."}
	raw := textToolCallArgsJSON(args)
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	var argv []string
	if err := json.Unmarshal(m["argv"], &argv); err != nil {
		t.Fatalf("argv should unmarshal as []string: %v", err)
	}
	if len(argv) != 2 || argv[0] != "go" || argv[1] != "version" {
		t.Fatalf("unexpected argv: %v", argv)
	}
	var cwd string
	if err := json.Unmarshal(m["cwd"], &cwd); err != nil {
		t.Fatalf("cwd should unmarshal as string: %v", err)
	}
	if cwd != "." {
		t.Fatalf("unexpected cwd: %s", cwd)
	}
}

func TestTextToolCallArgsJSON_PreservesBoolAndNumber(t *testing.T) {
	args := map[string]string{"recursive": "true", "depth": "3"}
	raw := textToolCallArgsJSON(args)
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if string(m["recursive"]) != "true" {
		t.Fatalf("expected raw true, got %s", m["recursive"])
	}
	if string(m["depth"]) != "3" {
		t.Fatalf("expected raw 3, got %s", m["depth"])
	}
}

func TestTextToolCallArgsJSON_Empty(t *testing.T) {
	raw := textToolCallArgsJSON(nil)
	if string(raw) != "{}" {
		t.Fatalf("expected {}, got %s", raw)
	}
}

func TestStripTextToolCallFragments(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "clean text unchanged",
			input: "Here is the implementation.",
			want:  "Here is the implementation.",
		},
		{
			name:  "strips tool_call wrapper",
			input: "some text\n<tool_call>\n<function=write_file>\n<parameter=path>main.go</parameter>\n</function>\n</tool_call>",
			want:  "some text",
		},
		{
			name:  "strips orphan parameter tags",
			input: "code output\n<parameter=path> cmd/list.go   </parameter>\n</tool_call>",
			want:  "code output",
		},
		{
			name:  "strips incomplete function open",
			input: "stuff <function=read_file> trailing",
			want:  "stuff  trailing",
		},
		{
			name:  "strips Qwen-style tool_call JSON block",
			input: "I'll read the file.\n<tool_call>{\"name\":\"read_file\",\"arguments\":{\"path\":\"main.go\"}}</tool_call>",
			want:  "I'll read the file.",
		},
		{
			name:  "strips multiple Qwen-style blocks",
			input: "<tool_call>{\"name\":\"read_file\",\"arguments\":{\"path\":\"a.go\"}}</tool_call>\n<tool_call>{\"name\":\"list_dir\",\"arguments\":{\"path\":\".\"}}</tool_call>",
			want:  "",
		},
		{
			name:  "preserves non-XML angle brackets",
			input: "use a <- b pattern",
			want:  "use a <- b pattern",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stripTextToolCallFragments(tc.input)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
