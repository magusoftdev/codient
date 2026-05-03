package codientcli

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3"

	"codient/internal/agent"
)

func TestParseAutoApprove(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want AutoApprovePolicy
	}{
		{"", AutoApproveOff},
		{"off", AutoApproveOff},
		{"exec", AutoApproveExec},
		{"fetch", AutoApproveFetch},
		{"all", AutoApproveAll},
		{"ALL", AutoApproveAll},
	} {
		got, err := ParseAutoApprove(tc.in)
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("%q: got %v want %v", tc.in, got, tc.want)
		}
	}
	if _, err := ParseAutoApprove("nope"); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseOutputFormat(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"", "text"},
		{"text", "text"},
		{"JSON", "json"},
		{"stream-json", "stream-json"},
	} {
		got, err := ParseOutputFormat(tc.in)
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("%q: got %q want %q", tc.in, got, tc.want)
		}
	}
	if _, err := ParseOutputFormat("xml"); err == nil {
		t.Fatal("expected error")
	}
}

func TestExitReasonForError(t *testing.T) {
	if exitReasonForError(nil) != "complete" {
		t.Fatal()
	}
	if exitReasonForError(fmt.Errorf("%w", agent.ErrMaxTurns)) != "max_turns" {
		t.Fatal()
	}
	if exitReasonForError(fmt.Errorf("%w", agent.ErrMaxCost)) != "max_cost" {
		t.Fatal()
	}
	if exitReasonForError(errors.New("x")) != "error" {
		t.Fatal()
	}
}

func TestSummarizeToolsAndFilesFromHistory(t *testing.T) {
	// Minimal assistant message with tool_calls as produced by json.Marshal on union.
	raw := `{"tool_calls":[{"function":{"arguments":"{\"path\":\"a.go\"}","name":"read_file"},"id":"1","index":0,"type":"function"}],"role":"assistant"}`
	var u openai.ChatCompletionMessageParamUnion
	if err := u.UnmarshalJSON([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	tools, files := summarizeToolsAndFilesFromHistory([]openai.ChatCompletionMessageParamUnion{u})
	if len(tools) != 1 || tools[0] != "read_file" {
		t.Fatalf("tools: %#v", tools)
	}
	if len(files) != 1 || files[0] != "a.go" {
		t.Fatalf("files: %#v", files)
	}
}

func TestAddPathsFromToolJSON(t *testing.T) {
	m := map[string]struct{}{}
	addPathsFromToolJSON("write_file", `{"path":"x.go","content":"z"}`, m)
	if _, ok := m["x.go"]; !ok {
		t.Fatal()
	}
}

func TestWriteHeadlessJSONResult(t *testing.T) {
	var b strings.Builder
	c := 0.01
	if err := writeHeadlessJSONResult(&b, "sess1", "/tmp/ws", "hi", []string{"echo"}, nil, 1, 2, 3, &c, nil); err != nil {
		t.Fatal(err)
	}
	s := b.String()
	if !strings.Contains(s, `"reply":"hi"`) || !strings.Contains(s, `"exit_reason":"complete"`) {
		t.Fatalf("%s", s)
	}
	if !strings.Contains(s, `"session_id":"sess1"`) || !strings.Contains(s, `"workspace":"/tmp/ws"`) {
		t.Fatalf("want session_id and workspace: %s", s)
	}
}
