package agentlog

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNew_NilWriter(t *testing.T) {
	l := New(nil)
	if l != nil {
		t.Fatal("expected nil logger for nil writer")
	}
}

func TestNew_NonNilWriter(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf)
	if l == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestNilLogger_NoOp(t *testing.T) {
	var l *Logger
	l.LLM(1, "gpt-4", time.Second, nil, 1, nil)
	l.ToolStart("read_file", nil)
	l.ToolEnd("read_file", time.Second, nil, nil)
}

func TestLLM_Success(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf)
	l.LLM(1, "gpt-4", 500*time.Millisecond, nil, 1, nil)

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if m["type"] != "llm" {
		t.Errorf("type = %v, want llm", m["type"])
	}
	if m["round"] != float64(1) {
		t.Errorf("round = %v, want 1", m["round"])
	}
	if m["model"] != "gpt-4" {
		t.Errorf("model = %v, want gpt-4", m["model"])
	}
	if _, ok := m["ts"]; !ok {
		t.Error("missing ts")
	}
	if _, ok := m["error"]; ok {
		t.Error("unexpected error key on success")
	}
}

func TestLLM_WithUsage(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf)
	u := &TokenUsage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150}
	l.LLM(1, "gpt-4o", 500*time.Millisecond, nil, 1, u)
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if m["prompt_tokens"] != float64(100) || m["completion_tokens"] != float64(50) {
		t.Fatalf("tokens: %+v", m)
	}
}

func TestLLM_Error(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf)
	l.LLM(2, "gpt-4", time.Second, errors.New("timeout"), 0, nil)

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if m["error"] != "timeout" {
		t.Errorf("error = %v, want timeout", m["error"])
	}
}

func TestToolStart(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf)
	l.ToolStart("grep", map[string]any{"pattern": "TODO"})

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if m["type"] != "tool_start" {
		t.Errorf("type = %v", m["type"])
	}
	if m["tool"] != "grep" {
		t.Errorf("tool = %v", m["tool"])
	}
	if m["pattern"] != "TODO" {
		t.Errorf("pattern = %v", m["pattern"])
	}
}

func TestToolEnd(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf)
	l.ToolEnd("read_file", 100*time.Millisecond, nil, map[string]any{"bytes": 42})

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if m["type"] != "tool_end" {
		t.Errorf("type = %v", m["type"])
	}
	if m["duration_ms"] != float64(100) {
		t.Errorf("duration_ms = %v", m["duration_ms"])
	}
	if m["bytes"] != float64(42) {
		t.Errorf("bytes = %v", m["bytes"])
	}
}

func TestToolEnd_Error(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf)
	l.ToolEnd("write_file", time.Second, errors.New("permission denied"), nil)

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if m["error"] != "permission denied" {
		t.Errorf("error = %v", m["error"])
	}
}

func TestSummarizeArgs_Empty(t *testing.T) {
	m := SummarizeArgs("read_file", nil)
	if m["arg_bytes"] != 0 {
		t.Errorf("arg_bytes = %v, want 0", m["arg_bytes"])
	}
	if _, ok := m["arg_sha256"]; ok {
		t.Error("unexpected sha256 for empty args")
	}
}

func TestSummarizeArgs_InvalidJSON(t *testing.T) {
	m := SummarizeArgs("read_file", []byte("not json"))
	if _, ok := m["arg_sha256"]; !ok {
		t.Error("expected sha256 even for invalid JSON")
	}
	if _, ok := m["path"]; ok {
		t.Error("should not have path for invalid JSON")
	}
}

func TestSummarizeArgs_ReadFile(t *testing.T) {
	m := SummarizeArgs("read_file", []byte(`{"path":"main.go"}`))
	if m["path"] != "main.go" {
		t.Errorf("path = %v", m["path"])
	}
}

func TestSummarizeArgs_RunCommand(t *testing.T) {
	m := SummarizeArgs("run_command", []byte(`{"argv":["go","test","./..."],"cwd":"."}`))
	argv, ok := m["argv"].([]string)
	if !ok || len(argv) != 3 {
		t.Fatalf("argv = %v", m["argv"])
	}
	if argv[0] != "go" || argv[1] != "test" || argv[2] != "./..." {
		t.Errorf("argv = %v", argv)
	}
	if m["cwd"] != "." {
		t.Errorf("cwd = %v", m["cwd"])
	}
}

func TestSummarizeArgs_RunShell(t *testing.T) {
	cmd := strings.Repeat("x", 200)
	m := SummarizeArgs("run_shell", []byte(`{"command":"`+cmd+`"}`))
	if m["command_len"] != 200 {
		t.Errorf("command_len = %v", m["command_len"])
	}
	prefix, ok := m["command_prefix"].(string)
	if !ok || len(prefix) != 120 {
		t.Errorf("command_prefix length = %d, want 120", len(prefix))
	}
}

func TestSummarizeArgs_WriteFile(t *testing.T) {
	m := SummarizeArgs("write_file", []byte(`{"path":"out.txt","content":"hello world"}`))
	if m["path"] != "out.txt" {
		t.Errorf("path = %v", m["path"])
	}
	if m["content_len"] != 11 {
		t.Errorf("content_len = %v", m["content_len"])
	}
}

func TestSummarizeArgs_WebSearch(t *testing.T) {
	m := SummarizeArgs("web_search", []byte(`{"query":"golang concurrency"}`))
	if m["query"] != "golang concurrency" {
		t.Errorf("query = %v", m["query"])
	}
}

func TestSummarizeArgs_FetchURL(t *testing.T) {
	m := SummarizeArgs("fetch_url", []byte(`{"url":"https://example.com"}`))
	if m["url"] != "https://example.com" {
		t.Errorf("url = %v", m["url"])
	}
}

func TestSummarizeArgs_Echo(t *testing.T) {
	short := SummarizeArgs("echo", []byte(`{"message":"hi"}`))
	if short["message"] != "hi" {
		t.Errorf("short message = %v", short["message"])
	}

	longMsg := strings.Repeat("a", 100)
	long := SummarizeArgs("echo", []byte(`{"message":"`+longMsg+`"}`))
	msg, ok := long["message"].(string)
	if !ok {
		t.Fatalf("message type = %T", long["message"])
	}
	if len([]rune(msg)) != 81 {
		t.Errorf("long message rune count = %d, want 81 (80 + ellipsis)", len([]rune(msg)))
	}
}

func TestSummarizeArgs_MovePath(t *testing.T) {
	m := SummarizeArgs("move_path", []byte(`{"from":"a.txt","to":"b.txt"}`))
	if m["from"] != "a.txt" || m["to"] != "b.txt" {
		t.Errorf("from=%v to=%v", m["from"], m["to"])
	}
}

func TestSummarizeArgs_Grep(t *testing.T) {
	m := SummarizeArgs("grep", []byte(`{"pattern":"TODO","path_prefix":"internal/"}`))
	if m["pattern"] != "TODO" {
		t.Errorf("pattern = %v", m["pattern"])
	}
	if m["path_prefix"] != "internal/" {
		t.Errorf("path_prefix = %v", m["path_prefix"])
	}
}

func TestSummarizeArgs_ListDir(t *testing.T) {
	m := SummarizeArgs("list_dir", []byte(`{"path":".","max_depth":3}`))
	if m["path"] != "." {
		t.Errorf("path = %v", m["path"])
	}
	if m["max_depth"] != 3 {
		t.Errorf("max_depth = %v", m["max_depth"])
	}
}

func TestSummarizeArgs_StrReplace(t *testing.T) {
	m := SummarizeArgs("str_replace", []byte(`{"path":"f.go","old_string":"foo","new_string":"bar"}`))
	if m["path"] != "f.go" {
		t.Errorf("path = %v", m["path"])
	}
	if m["old_string_len"] != 3 {
		t.Errorf("old_string_len = %v", m["old_string_len"])
	}
	if m["new_string_len"] != 3 {
		t.Errorf("new_string_len = %v", m["new_string_len"])
	}
}

func TestSummarizeArgs_DelegateTask(t *testing.T) {
	m := SummarizeArgs("delegate_task", []byte(`{"mode":"ask","task":"find all TODO comments in the codebase"}`))
	if m["mode"] != "ask" {
		t.Errorf("mode = %v", m["mode"])
	}
	if m["task"] != "find all TODO comments in the codebase" {
		t.Errorf("task = %v", m["task"])
	}
	if _, ok := m["task_prefix"]; ok {
		t.Error("short task should not be truncated")
	}
}

func TestSummarizeArgs_DelegateTask_LongTask(t *testing.T) {
	longTask := strings.Repeat("x", 200)
	m := SummarizeArgs("delegate_task", []byte(`{"mode":"build","task":"`+longTask+`"}`))
	if m["mode"] != "build" {
		t.Errorf("mode = %v", m["mode"])
	}
	prefix, ok := m["task_prefix"].(string)
	if !ok || len(prefix) != 120 {
		t.Errorf("task_prefix length = %d, want 120", len(prefix))
	}
	if _, ok := m["task"]; ok {
		t.Error("long task should use task_prefix, not task")
	}
}

func TestWithSubAgent_NilLogger(t *testing.T) {
	var l *Logger
	child := l.WithSubAgent("ask", "gpt-4")
	if child != nil {
		t.Fatal("WithSubAgent on nil logger should return nil")
	}
	// nil child should not panic on emit
	child.LLM(1, "gpt-4", 0, nil, 1, nil)
	child.ToolStart("echo", nil)
	child.ToolEnd("echo", 0, nil, nil)
}

func TestWithSubAgent_TagsEvents(t *testing.T) {
	var buf bytes.Buffer
	parent := New(&buf)
	child := parent.WithSubAgent("ask", "gpt-4.1-mini")

	child.LLM(1, "gpt-4.1-mini", 0, nil, 1, nil)

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if m["subagent"] != true {
		t.Errorf("subagent = %v, want true", m["subagent"])
	}
	if m["subagent_mode"] != "ask" {
		t.Errorf("subagent_mode = %v", m["subagent_mode"])
	}
	if m["subagent_model"] != "gpt-4.1-mini" {
		t.Errorf("subagent_model = %v", m["subagent_model"])
	}
	if m["type"] != "llm" {
		t.Errorf("type = %v", m["type"])
	}
}

func TestWithSubAgent_ParentUntagged(t *testing.T) {
	var buf bytes.Buffer
	parent := New(&buf)
	_ = parent.WithSubAgent("ask", "gpt-4.1-mini")

	parent.LLM(1, "gpt-4", 0, nil, 1, nil)

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := m["subagent"]; ok {
		t.Error("parent logger should not have subagent tag")
	}
}

func TestWithSubAgent_ToolEvents(t *testing.T) {
	var buf bytes.Buffer
	parent := New(&buf)
	child := parent.WithSubAgent("build", "gpt-4.1")

	child.ToolStart("read_file", map[string]any{"path": "main.go"})

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	var m map[string]any
	json.Unmarshal([]byte(lines[0]), &m)
	if m["type"] != "tool_start" {
		t.Errorf("type = %v", m["type"])
	}
	if m["subagent"] != true {
		t.Error("tool_start should be tagged as subagent")
	}
	if m["subagent_mode"] != "build" {
		t.Errorf("subagent_mode = %v", m["subagent_mode"])
	}
}
