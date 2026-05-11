//go:build integration

package codientcli_test

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func skipUnlessIntegrationACP(t *testing.T) {
	t.Helper()
	if os.Getenv("CODIENT_INTEGRATION") != "1" {
		t.Skip("set CODIENT_INTEGRATION=1 to run live ACP subprocess tests")
	}
}

func skipUnlessStrictToolsACP(t *testing.T) {
	t.Helper()
	if os.Getenv("CODIENT_INTEGRATION_STRICT_TOOLS") != "1" {
		t.Skip("set CODIENT_INTEGRATION_STRICT_TOOLS=1 for tool-based ACP tests")
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	mod := strings.TrimSpace(string(out))
	if mod == "" {
		t.Fatal("GOMOD empty")
	}
	return filepath.Dir(mod)
}

func codientExeName() string {
	if runtime.GOOS == "windows" {
		return "codient_acp_integration.exe"
	}
	return "codient_acp_integration"
}

func buildCodientBinary(t *testing.T) string {
	t.Helper()
	root := moduleRoot(t)
	bin := filepath.Join(t.TempDir(), codientExeName())
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/codient")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

type acpHarness struct {
	t      *testing.T
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	nextID int
	idMu   sync.Mutex
}

func startACP(t *testing.T, workspace string) *acpHarness {
	t.Helper()
	return startACPWithEnv(t, workspace, os.Environ())
}

func startACPWithEnv(t *testing.T, workspace string, env []string) *acpHarness {
	t.Helper()
	return startACPWithEnvMode(t, workspace, env, "")
}

func startACPWithMode(t *testing.T, workspace, _ string) *acpHarness {
	t.Helper()
	return startACPWithEnvMode(t, workspace, os.Environ(), "")
}

// startACPWithEnvMode is kept for source compatibility with older harness
// callers but now ignores the mode argument: every ACP session runs the
// orchestrator and there is no -mode flag on the binary anymore.
func startACPWithEnvMode(t *testing.T, workspace string, env []string, _ string) *acpHarness {
	t.Helper()
	h := &acpHarness{t: t}
	bin := buildCodientBinary(t)
	root := moduleRoot(t)
	ws, err := filepath.Abs(workspace)
	if err != nil {
		t.Fatal(err)
	}
	h.cmd = exec.Command(bin, "-acp", "-plain", "-workspace", ws, "-stream-reply=true")
	h.cmd.Dir = root
	h.cmd.Env = append([]string(nil), env...)
	h.cmd.Stderr = io.Discard
	stdin, err := h.cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	h.stdin = stdin
	stdout, err := h.cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	h.stdout = bufio.NewReader(stdout)
	if err := h.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = h.stdin.Close()
		_ = h.cmd.Process.Kill()
		_, _ = h.cmd.Process.Wait()
	})
	return h
}

func (h *acpHarness) nextRPCID() int {
	h.idMu.Lock()
	defer h.idMu.Unlock()
	h.nextID++
	return h.nextID
}

func (h *acpHarness) writeJSON(v any) {
	h.t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		h.t.Fatal(err)
	}
	if _, err := h.stdin.Write(append(b, '\n')); err != nil {
		h.t.Fatal(err)
	}
}

type rpcLine struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (h *acpHarness) readLine(timeout time.Duration) string {
	h.t.Helper()
	type res struct {
		s   string
		err error
	}
	ch := make(chan res, 1)
	go func() {
		s, err := h.stdout.ReadString('\n')
		ch <- res{strings.TrimRight(s, "\r\n"), err}
	}()
	select {
	case v := <-ch:
		if v.err != nil && v.err != io.EOF {
			h.t.Fatalf("read stdout: %v", v.err)
		}
		return v.s
	case <-time.After(timeout):
		h.t.Fatalf("timeout after %v waiting for ACP stdout line", timeout)
	}
	return ""
}

// readUntilRPCResult reads NDJSON until a response with matching id (result or error).
// Lines with method session/update are appended to notifications (raw JSON per line).
func (h *acpHarness) readUntilRPCResult(wantID int, total time.Duration) (result json.RawMessage, rpcErr *struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}, notifications []string) {
	h.t.Helper()
	deadline := time.Now().Add(total)
	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		if remaining > 30*time.Second {
			remaining = 30 * time.Second
		}
		if remaining < time.Second {
			remaining = time.Second
		}
		line := h.readLine(remaining)
		if line == "" {
			continue
		}
		var wrap rpcLine
		if err := json.Unmarshal([]byte(line), &wrap); err != nil {
			h.t.Logf("skip non-JSON line: %q", line[:min(120, len(line))])
			continue
		}
		// Agent → client JSON-RPC requests (e.g. unity/*) appear on stdout; reply on stdin so CallClient completes.
		if wrap.ID != nil && wrap.Method != "" && wrap.Result == nil && wrap.Error == nil {
			if replyACPClientRequest(h, wrap.Method, *wrap.ID) {
				continue
			}
		}
		if wrap.Method == "session/update" {
			notifications = append(notifications, line)
			continue
		}
		if wrap.ID != nil && *wrap.ID == wantID {
			return wrap.Result, wrap.Error, notifications
		}
	}
	h.t.Fatalf("timeout waiting for JSON-RPC id %d", wantID)
	return nil, nil, notifications
}

func (h *acpHarness) handshake() {
	h.t.Helper()
	id := h.nextRPCID()
	h.writeJSON(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": 1,
			"clientCapabilities": map[string]any{
				"fs": map[string]any{"readTextFile": true, "writeTextFile": true},
			},
			"clientInfo": map[string]any{"name": "integration-test", "version": "0"},
		},
	})
	res, errObj, _ := h.readUntilRPCResult(id, 2*time.Minute)
	if errObj != nil {
		h.t.Fatalf("initialize error: %d %s", errObj.Code, errObj.Message)
	}
	var parsed struct {
		AgentInfo struct {
			Version string `json:"version"`
		} `json:"agentInfo"`
	}
	if err := json.Unmarshal(res, &parsed); err != nil {
		h.t.Fatalf("initialize result: %v", err)
	}
	if strings.TrimSpace(parsed.AgentInfo.Version) == "" {
		h.t.Fatalf("initialize: expected agentInfo.version in %#v", string(res))
	}
}

func (h *acpHarness) agentListModels() []string {
	h.t.Helper()
	id := h.nextRPCID()
	h.writeJSON(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "agent/list_models",
		"params":  map[string]any{},
	})
	res, errObj, _ := h.readUntilRPCResult(id, 2*time.Minute)
	if errObj != nil {
		h.t.Fatalf("agent/list_models error: %d %s", errObj.Code, errObj.Message)
	}
	var parsed struct {
		Models []struct {
			ID string `json:"id"`
		} `json:"models"`
	}
	if err := json.Unmarshal(res, &parsed); err != nil {
		h.t.Fatalf("agent/list_models decode: %v (raw %s)", err, string(res))
	}
	var out []string
	for _, m := range parsed.Models {
		s := strings.TrimSpace(m.ID)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func writeACPIntegrationConfigJSON(t *testing.T, stateDir string, cfg map[string]any) {
	t.Helper()
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal codient config json: %v", err)
	}
	path := filepath.Join(stateDir, "config.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func runACPAgentListModelsSmoke(t *testing.T, cfg map[string]any, want []string) {
	t.Helper()
	stateDir := t.TempDir()
	writeACPIntegrationConfigJSON(t, stateDir, cfg)
	dir := t.TempDir()
	env := append(os.Environ(), "CODIENT_STATE_DIR="+stateDir)
	h := startACPWithEnv(t, dir, env)
	h.handshake()
	got := h.agentListModels()
	sort.Strings(got)
	wantSorted := append([]string(nil), want...)
	sort.Strings(wantSorted)
	if !reflect.DeepEqual(got, wantSorted) {
		t.Fatalf("agent/list_models got %#v want %#v", got, wantSorted)
	}
}

func (h *acpHarness) sessionNew(cwd string) string {
	return h.sessionNewWithMode(cwd, "")
}

// sessionNewWithMode creates a session with an explicit mode (e.g. "auto") in
// the params. Returns the new session id; fails the test on any RPC error.
func (h *acpHarness) sessionNewWithMode(cwd, mode string) string {
	h.t.Helper()
	id := h.nextRPCID()
	params := map[string]any{
		"cwd":        cwd,
		"mcpServers": []any{},
	}
	if mode != "" {
		params["mode"] = mode
	}
	h.writeJSON(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "session/new",
		"params":  params,
	})
	res, errObj, _ := h.readUntilRPCResult(id, 5*time.Minute)
	if errObj != nil {
		h.t.Fatalf("session/new error: %d %s", errObj.Code, errObj.Message)
	}
	var out struct {
		SessionID string `json:"sessionId"`
		Mode      string `json:"mode"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		h.t.Fatal(err)
	}
	if out.SessionID == "" {
		h.t.Fatalf("session/new: missing sessionId in %s", string(res))
	}
	if mode != "" && out.Mode != mode {
		h.t.Fatalf("session/new: requested mode %q but server echoed %q", mode, out.Mode)
	}
	return out.SessionID
}

func (h *acpHarness) sessionSetModel(sessionID, model string) string {
	h.t.Helper()
	return h.sessionSetModelExt(sessionID, model, nil)
}

// sessionSetModelExt calls session/set_model. If preload is non-nil, it is sent as the "preload" JSON field.
func (h *acpHarness) sessionSetModelExt(sessionID, model string, preload *bool) string {
	h.t.Helper()
	id := h.nextRPCID()
	params := map[string]any{"sessionId": sessionID}
	if model != "" {
		params["model"] = model
	}
	if preload != nil {
		params["preload"] = *preload
	}
	h.writeJSON(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "session/set_model",
		"params":  params,
	})
	res, errObj, _ := h.readUntilRPCResult(id, 2*time.Minute)
	if errObj != nil {
		h.t.Fatalf("session/set_model error: %d %s", errObj.Code, errObj.Message)
	}
	var parsed struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(res, &parsed); err != nil {
		h.t.Fatalf("session/set_model decode: %v (raw %s)", err, string(res))
	}
	return parsed.Model
}

func (h *acpHarness) sessionSetModelExpectError(sessionID, model string) (code int, msg string) {
	h.t.Helper()
	id := h.nextRPCID()
	params := map[string]any{"sessionId": sessionID}
	if model != "" {
		params["model"] = model
	}
	h.writeJSON(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "session/set_model",
		"params":  params,
	})
	_, errObj, _ := h.readUntilRPCResult(id, 2*time.Minute)
	if errObj == nil {
		h.t.Fatal("session/set_model: expected error")
	}
	return errObj.Code, errObj.Message
}

func (h *acpHarness) sessionPrompt(sessionID, userText string) (stopReason, reply string, notes []string) {
	h.t.Helper()
	id := h.nextRPCID()
	prompt := []map[string]any{{"type": "text", "text": userText}}
	h.writeJSON(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "session/prompt",
		"params": map[string]any{
			"sessionId": sessionID,
			"prompt":    prompt,
		},
	})
	res, errObj, notes := h.readUntilRPCResult(id, 8*time.Minute)
	if errObj != nil {
		h.t.Fatalf("session/prompt error: %d %s", errObj.Code, errObj.Message)
	}
	var parsed struct {
		StopReason string `json:"stopReason"`
		Reply      string `json:"reply"`
	}
	if err := json.Unmarshal(res, &parsed); err != nil {
		h.t.Fatal(err)
	}
	return parsed.StopReason, parsed.Reply, notes
}

func (h *acpHarness) sendCancel(sessionID string) {
	h.t.Helper()
	h.writeJSON(map[string]any{
		"jsonrpc": "2.0",
		"method":  "session/cancel",
		"params": map[string]any{
			"sessionId": sessionID,
		},
	})
}

func TestACPIntegration_InitializeHandshake(t *testing.T) {
	skipUnlessIntegrationACP(t)
	dir := t.TempDir()
	h := startACP(t, dir)
	h.handshake()
}

func TestACPIntegration_InitializeDefaultChatModel(t *testing.T) {
	skipUnlessIntegrationACP(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"aaa-listed-first"},{"id":"zzz-config-default"}]}`))
	}))
	defer srv.Close()
	stateDir := t.TempDir()
	writeACPIntegrationConfigJSON(t, stateDir, map[string]any{
		"base_url": srv.URL + "/v1",
		"api_key":  "integration-test",
		"model":    "zzz-config-default",
	})
	dir := t.TempDir()
	h := startACPWithEnv(t, dir, append(os.Environ(), "CODIENT_STATE_DIR="+stateDir))
	id := h.nextRPCID()
	h.writeJSON(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": 1,
			"clientCapabilities": map[string]any{
				"fs": map[string]any{"readTextFile": true, "writeTextFile": true},
			},
			"clientInfo": map[string]any{"name": "integration-test", "version": "0"},
		},
	})
	res, errObj, _ := h.readUntilRPCResult(id, 2*time.Minute)
	if errObj != nil {
		t.Fatalf("initialize error: %d %s", errObj.Code, errObj.Message)
	}
	var parsed struct {
		DefaultChatModel string `json:"defaultChatModel"`
	}
	if err := json.Unmarshal(res, &parsed); err != nil {
		t.Fatalf("initialize result: %v", err)
	}
	if parsed.DefaultChatModel != "zzz-config-default" {
		t.Fatalf("defaultChatModel got %q want zzz-config-default (must match config, not first list_models id)", parsed.DefaultChatModel)
	}
}

func TestACPIntegration_SessionSetModelRPC(t *testing.T) {
	skipUnlessIntegrationACP(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/models" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"mock-openai-model"},{"id":"alt-model"}]}`))
		case r.URL.Path == "/v1/chat/completions" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"warm","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"."},"finish_reason":"stop"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	stateDir := t.TempDir()
	writeACPIntegrationConfigJSON(t, stateDir, map[string]any{
		"base_url": srv.URL + "/v1",
		"api_key":  "integration-test",
		"model":    "mock-openai-model",
	})
	dir := t.TempDir()
	h := startACPWithEnv(t, dir, append(os.Environ(), "CODIENT_STATE_DIR="+stateDir))
	h.handshake()
	ws, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	sid := h.sessionNew(ws)
	if got := h.sessionSetModel(sid, "  alt-model  "); got != "alt-model" {
		t.Fatalf("session/set_model model got %q want %q", got, "alt-model")
	}
	if got := h.sessionSetModel(sid, ""); got != "" {
		t.Fatalf("session/set_model clear default got %q want empty", got)
	}
	code, msg := h.sessionSetModelExpectError("sess_nonexistent", "x")
	if code != -32001 || !strings.Contains(msg, "unknown session") {
		t.Fatalf("session/set_model unknown session: code=%d msg=%q", code, msg)
	}
}

func TestACPIntegration_SessionSetModelPreloadCountsCompletions(t *testing.T) {
	skipUnlessIntegrationACP(t)
	var warmCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/models" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"mock-openai-model"},{"id":"alt-model"}]}`))
		case r.URL.Path == "/v1/chat/completions" && r.Method == http.MethodPost:
			warmCount.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"warm","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"."},"finish_reason":"stop"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	stateDir := t.TempDir()
	writeACPIntegrationConfigJSON(t, stateDir, map[string]any{
		"base_url": srv.URL + "/v1",
		"api_key":  "integration-test",
		"model":    "mock-openai-model",
	})
	dir := t.TempDir()
	h := startACPWithEnv(t, dir, append(os.Environ(), "CODIENT_STATE_DIR="+stateDir))
	h.handshake()
	ws, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	sid := h.sessionNew(ws)
	_ = h.sessionSetModel(sid, "alt-model")
	if n := warmCount.Load(); n != 1 {
		t.Fatalf("expected 1 warmup completion after set_model, got %d", n)
	}
}

func TestACPIntegration_SessionSetModelPreloadRollback(t *testing.T) {
	skipUnlessIntegrationACP(t)
	var warmCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/models" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"mock-openai-model"},{"id":"alt-model"},{"id":"bad-model"}]}`))
		case r.URL.Path == "/v1/chat/completions" && r.Method == http.MethodPost:
			warmCount.Add(1)
			body, _ := io.ReadAll(r.Body)
			_ = r.Body.Close()
			// Fail every attempt to load bad-model (SDK may retry on 5xx).
			if strings.Contains(string(body), "bad-model") {
				http.Error(w, "warm failed", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"warm","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"."},"finish_reason":"stop"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	stateDir := t.TempDir()
	writeACPIntegrationConfigJSON(t, stateDir, map[string]any{
		"base_url": srv.URL + "/v1",
		"api_key":  "integration-test",
		"model":    "mock-openai-model",
	})
	dir := t.TempDir()
	h := startACPWithEnv(t, dir, append(os.Environ(), "CODIENT_STATE_DIR="+stateDir))
	h.handshake()
	ws, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	sid := h.sessionNew(ws)
	_ = h.sessionSetModel(sid, "alt-model")
	code, msg := h.sessionSetModelExpectError(sid, "bad-model")
	if code != -32603 || !strings.Contains(msg, "preload") {
		t.Fatalf("expected preload error code=%d msg contains preload: %q", code, msg)
	}
	if got := h.sessionSetModel(sid, "alt-model"); got != "alt-model" {
		t.Fatalf("after failed switch session should still accept alt-model warm; got %q", got)
	}
}

func TestACPIntegration_SessionSetModelPreloadDisabled(t *testing.T) {
	skipUnlessIntegrationACP(t)
	var completionHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/models" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"mock-openai-model"},{"id":"alt-model"}]}`))
		case r.URL.Path == "/v1/chat/completions" && r.Method == http.MethodPost:
			completionHits.Add(1)
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	stateDir := t.TempDir()
	writeACPIntegrationConfigJSON(t, stateDir, map[string]any{
		"base_url":                       srv.URL + "/v1",
		"api_key":                        "integration-test",
		"model":                          "mock-openai-model",
		"acp_preload_model_on_set_model": false,
	})
	dir := t.TempDir()
	h := startACPWithEnv(t, dir, append(os.Environ(), "CODIENT_STATE_DIR="+stateDir))
	h.handshake()
	ws, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	sid := h.sessionNew(ws)
	if got := h.sessionSetModel(sid, "alt-model"); got != "alt-model" {
		t.Fatalf("set_model: %q", got)
	}
	if completionHits.Load() != 0 {
		t.Fatalf("expected no chat completion when preload disabled, got %d", completionHits.Load())
	}
}

func TestACPIntegration_SessionSetModelOllamaUnloadsBeforeWarm(t *testing.T) {
	skipUnlessIntegrationACP(t)
	var order []string
	var orderMu sync.Mutex
	appendOrder := func(step string) {
		orderMu.Lock()
		order = append(order, step)
		orderMu.Unlock()
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/models" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"mock-openai-model"},{"id":"alt-model"}]}`))
		case r.URL.Path == "/api/generate" && r.Method == http.MethodPost:
			appendOrder("generate")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"model":"m","created_at":"t","response":"","done":true,"done_reason":"unload"}`))
		case r.URL.Path == "/v1/chat/completions" && r.Method == http.MethodPost:
			appendOrder("chat")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"warm","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"."},"finish_reason":"stop"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	stateDir := t.TempDir()
	writeACPIntegrationConfigJSON(t, stateDir, map[string]any{
		"base_url": srv.URL + "/v1",
		"api_key":  "integration-test",
		"model":    "mock-openai-model",
	})
	dir := t.TempDir()
	h := startACPWithEnv(t, dir, append(os.Environ(), "CODIENT_STATE_DIR="+stateDir))
	h.handshake()
	ws, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	sid := h.sessionNew(ws)
	_ = h.sessionSetModel(sid, "alt-model")
	orderMu.Lock()
	got := append([]string(nil), order...)
	orderMu.Unlock()
	if len(got) < 2 || got[0] != "generate" || got[1] != "chat" {
		t.Fatalf("expected unload (POST /api/generate) then warm (chat completions); got %#v", got)
	}
}

func TestACPIntegration_SessionSetModelPreloadRPCFalse(t *testing.T) {
	skipUnlessIntegrationACP(t)
	var completionHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/models" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"mock-openai-model"},{"id":"alt-model"}]}`))
		case r.URL.Path == "/v1/chat/completions" && r.Method == http.MethodPost:
			completionHits.Add(1)
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	stateDir := t.TempDir()
	writeACPIntegrationConfigJSON(t, stateDir, map[string]any{
		"base_url": srv.URL + "/v1",
		"api_key":  "integration-test",
		"model":    "mock-openai-model",
	})
	dir := t.TempDir()
	h := startACPWithEnv(t, dir, append(os.Environ(), "CODIENT_STATE_DIR="+stateDir))
	h.handshake()
	ws, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	sid := h.sessionNew(ws)
	pre := false
	if got := h.sessionSetModelExt(sid, "alt-model", &pre); got != "alt-model" {
		t.Fatalf("set_model: %q", got)
	}
	if completionHits.Load() != 0 {
		t.Fatalf("expected no chat completion when preload=false in RPC, got %d", completionHits.Load())
	}
}

func TestACPIntegration_AgentListModelsMockUpstream(t *testing.T) {
	skipUnlessIntegrationACP(t)

	t.Run("openai_data_shape", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/models" || r.Method != http.MethodGet {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"mock-openai-model"}]}`))
		}))
		defer srv.Close()
		runACPAgentListModelsSmoke(t, map[string]any{
			"base_url": srv.URL + "/v1",
			"api_key":  "integration-test",
			"model":    "mock-openai-model",
		}, []string{"mock-openai-model"})
	})

	t.Run("top_level_models_array", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/models" || r.Method != http.MethodGet {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"models":[{"name":"named-only-model"}]}`))
		}))
		defer srv.Close()
		runACPAgentListModelsSmoke(t, map[string]any{
			"base_url": srv.URL + "/v1",
			"api_key":  "integration-test",
			"model":    "named-only-model",
		}, []string{"named-only-model"})
	})

	t.Run("lm_studio_models_key_only", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/models" || r.Method != http.MethodGet {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"models":[{"type":"llm","key":"lm-studio/key-a"},{"type":"llm","key":"lm-studio/key-b"}]}`))
		}))
		defer srv.Close()
		runACPAgentListModelsSmoke(t, map[string]any{
			"base_url": srv.URL + "/v1",
			"api_key":  "integration-test",
			"model":    "lm-studio/key-a",
		}, []string{"lm-studio/key-a", "lm-studio/key-b"})
	})

	t.Run("per_mode_base_url_does_not_break_list", func(t *testing.T) {
		t.Parallel()
		// agent/list_models uses the top-level client, so per-mode overrides
		// must not interfere. Point the top-level URL at a mock and verify
		// that the presence of a per-mode base_url doesn't break listing.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/models" || r.Method != http.MethodGet {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"top-level-model"}]}`))
		}))
		defer srv.Close()
		runACPAgentListModelsSmoke(t, map[string]any{
			"base_url": srv.URL + "/v1",
			"api_key":  "integration-test",
			"model":    "top-level-model",
			"models": map[string]any{
				"build": map[string]any{
					"base_url": "http://127.0.0.1:19999/v1",
					"api_key":  "k",
				},
			},
		}, []string{"top-level-model"})
	})
}

func TestACPIntegration_SessionNewAndPromptReply(t *testing.T) {
	skipUnlessIntegrationACP(t)
	if testing.Short() {
		t.Skip("skipping live LLM call in -short mode")
	}
	dir := t.TempDir()
	h := startACP(t, dir)
	h.handshake()
	ws, _ := filepath.Abs(dir)
	sid := h.sessionNew(ws)
	stop, reply, notes := h.sessionPrompt(sid, "Reply with a single short greeting (one line). Do not call tools.")
	if stop != "" && stop != "end_turn" {
		t.Logf("stopReason=%q", stop)
	}
	if strings.TrimSpace(reply) == "" {
		t.Fatalf("expected non-empty reply; notifications=%d", len(notes))
	}
}

func TestACPIntegration_ToolCallNotifications(t *testing.T) {
	skipUnlessIntegrationACP(t)
	skipUnlessStrictToolsACP(t)
	if testing.Short() {
		t.Skip("skipping live LLM call in -short mode")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "marker_acp.txt"), []byte("ACP_MARKER_44dd"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := startACP(t, dir)
	h.handshake()
	ws, _ := filepath.Abs(dir)
	sid := h.sessionNew(ws)
	user := "You have list_dir. Call list_dir with path \".\" and max_depth 0, then say whether a file named marker_acp.txt appears in the listing."
	stop, reply, notes := h.sessionPrompt(sid, user)
	if stop != "" && stop != "end_turn" {
		t.Logf("stopReason=%q", stop)
	}
	var sawTool, sawUpdate bool
	for _, raw := range notes {
		var u struct {
			Params struct {
				Update struct {
					SessionUpdate string `json:"sessionUpdate"`
					Status        string `json:"status"`
					Title         string `json:"title"`
				} `json:"update"`
			} `json:"params"`
		}
		if err := json.Unmarshal([]byte(raw), &u); err != nil {
			continue
		}
		switch u.Params.Update.SessionUpdate {
		case "tool_call":
			sawTool = true
			if strings.TrimSpace(u.Params.Update.Title) == "" {
				t.Fatalf("tool_call missing title: %s", raw)
			}
		case "tool_call_update":
			sawUpdate = true
			if u.Params.Update.Status != "completed" && u.Params.Update.Status != "failed" {
				t.Logf("tool_call_update status=%q", u.Params.Update.Status)
			}
		}
	}
	if !sawTool {
		t.Fatalf("expected at least one tool_call session/update; got %d notifications", len(notes))
	}
	if !sawUpdate {
		t.Log("no tool_call_update observed (server may omit in some cases)")
	}
	if !strings.Contains(reply, "marker_acp.txt") && !strings.Contains(strings.ToLower(reply), "marker") {
		t.Logf("reply may still be valid: %q", reply)
	}
}

func TestACPIntegration_IntentNotification(t *testing.T) {
	skipUnlessIntegrationACP(t)
	skipUnlessStrictToolsACP(t)
	if testing.Short() {
		t.Skip("skipping live LLM call in -short mode")
	}
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "intent_probe.txt"), []byte("x"), 0o644)
	h := startACP(t, dir)
	h.handshake()
	ws, _ := filepath.Abs(dir)
	sid := h.sessionNew(ws)
	_, _, notes := h.sessionPrompt(sid, "Use list_dir on \".\" with max_depth 0, then briefly describe what you will do before listing (one sentence), then list.")
	var firstToolIdx, firstPlanOrChunkIdx = -1, -1
	for i, raw := range notes {
		var u struct {
			Params struct {
				Update struct {
					SessionUpdate string `json:"sessionUpdate"`
				} `json:"update"`
			} `json:"params"`
		}
		if err := json.Unmarshal([]byte(raw), &u); err != nil {
			continue
		}
		su := u.Params.Update.SessionUpdate
		if su == "tool_call" && firstToolIdx < 0 {
			firstToolIdx = i
		}
		if (su == "plan" || su == "agent_message_chunk") && firstPlanOrChunkIdx < 0 {
			firstPlanOrChunkIdx = i
		}
	}
	if firstToolIdx < 0 {
		t.Fatal("expected tool_call notification")
	}
	if firstPlanOrChunkIdx < 0 || firstPlanOrChunkIdx > firstToolIdx {
		t.Logf("intent/plan before tool_call: planIdx=%d toolIdx=%d (model-dependent)", firstPlanOrChunkIdx, firstToolIdx)
	}
}

func TestACPIntegration_MultiRoundDoesNotError(t *testing.T) {
	skipUnlessIntegrationACP(t)
	skipUnlessStrictToolsACP(t)
	if testing.Short() {
		t.Skip("skipping live LLM call in -short mode")
	}
	const needle = "ACP_MULTI_ROUND_62ee"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "round_two.txt"), []byte(needle), 0o644); err != nil {
		t.Fatal(err)
	}
	h := startACP(t, dir)
	h.handshake()
	ws, _ := filepath.Abs(dir)
	sid := h.sessionNew(ws)
	user := "You have list_dir and read_file. Step 1: call list_dir with path \".\" and max_depth 0. Step 2: call read_file with path \"round_two.txt\". After both tools return, your reply MUST contain the exact substring " + needle + "."
	stop, reply, _ := h.sessionPrompt(sid, user)
	if stop != "" && stop != "end_turn" {
		t.Fatalf("unexpected stopReason %q reply=%q", stop, reply)
	}
	if !strings.Contains(reply, needle) {
		t.Fatalf("expected reply to contain %q; got %q", needle, reply)
	}
}

func TestACPIntegration_CancelMidTurn(t *testing.T) {
	skipUnlessIntegrationACP(t)
	if testing.Short() {
		t.Skip("skipping live LLM call in -short mode")
	}
	dir := t.TempDir()
	h := startACP(t, dir)
	h.handshake()
	ws, _ := filepath.Abs(dir)
	sid := h.sessionNew(ws)

	id := h.nextRPCID()
	prompt := []map[string]any{{"type": "text", "text": "Write a very long essay (at least 2000 words) about software testing. Do not call tools."}}
	h.writeJSON(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "session/prompt",
		"params": map[string]any{
			"sessionId": sid,
			"prompt":    prompt,
		},
	})

	go func() {
		time.Sleep(400 * time.Millisecond)
		h.sendCancel(sid)
	}()

	res, errObj, _ := h.readUntilRPCResult(id, 3*time.Minute)
	if errObj != nil {
		t.Fatalf("session/prompt error: %d %s", errObj.Code, errObj.Message)
	}
	var parsed struct {
		StopReason string `json:"stopReason"`
	}
	if err := json.Unmarshal(res, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.StopReason != "cancelled" {
		t.Fatalf("expected stopReason cancelled, got %q (raw=%s)", parsed.StopReason, string(res))
	}
}

// replyACPClientRequest answers agent→client JSON-RPC so CallClient-based tools (unity/*) can complete in integration tests.
func replyACPClientRequest(h *acpHarness, method string, id int) bool {
	if !strings.HasPrefix(method, "unity/") {
		return false
	}
	var res any
	switch method {
	case "unity/query_scene_hierarchy":
		res = map[string]any{"nodes": []any{}}
	case "unity/search_asset_database":
		res = map[string]any{"hits": []any{}, "truncated": false}
	case "unity/inspect_component":
		res = map[string]any{"properties": map[string]any{}, "truncated": false}
	case "unity/list_loaded_scenes":
		res = map[string]any{"scenes": []any{}, "activeScenePath": ""}
	case "unity/query_prefab_hierarchy":
		res = map[string]any{"nodes": []any{}}
	case "unity/get_console_errors":
		res = map[string]any{"entries": []any{}, "note": "mock"}
	case "unity/summarize_project_packages":
		res = map[string]any{"manifestPreview": "", "asmdefPaths": []any{}, "asmdefListTruncated": false}
	case "unity/apply_actions":
		res = map[string]any{"cancelled": true, "message": "integration test mock", "results": []any{}}
	default:
		res = map[string]any{"mock": true, "method": method}
	}
	h.writeJSON(map[string]any{"jsonrpc": "2.0", "id": id, "result": res})
	return true
}

// TestACPIntegration_SessionNewAcceptsAutoMode is a mock-server check that
// session/new accepts mode="auto" (the only externally-visible mode) and
// echoes it back. Validates the auto-only wiring without exercising the LLM.
func TestACPIntegration_SessionNewAcceptsAutoMode(t *testing.T) {
	skipUnlessIntegrationACP(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/models" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"mock-openai-model"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	stateDir := t.TempDir()
	writeACPIntegrationConfigJSON(t, stateDir, map[string]any{
		"base_url": srv.URL + "/v1",
		"api_key":  "integration-test",
		"model":    "mock-openai-model",
	})
	dir := t.TempDir()
	h := startACPWithEnvMode(t, dir, append(os.Environ(), "CODIENT_STATE_DIR="+stateDir), "auto")
	h.handshake()
	ws, _ := filepath.Abs(dir)
	// session/new with explicit mode=auto must round-trip.
	_ = h.sessionNewWithMode(ws, "auto")
}

// TestACPIntegration_SessionSetModeRemoved verifies the session/set_mode RPC is
// no longer routed (every session is auto-mode and the orchestrator picks an
// internal mode per turn). Older Codient Unity builds that still send the RPC
// must receive a clear "method not found" response so they can fall back.
func TestACPIntegration_SessionSetModeRemoved(t *testing.T) {
	skipUnlessIntegrationACP(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/models" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"mock-openai-model"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	stateDir := t.TempDir()
	writeACPIntegrationConfigJSON(t, stateDir, map[string]any{
		"base_url": srv.URL + "/v1",
		"api_key":  "integration-test",
		"model":    "mock-openai-model",
	})
	dir := t.TempDir()
	h := startACPWithEnv(t, dir, append(os.Environ(), "CODIENT_STATE_DIR="+stateDir))
	h.handshake()
	ws, _ := filepath.Abs(dir)
	sid := h.sessionNew(ws)

	id := h.nextRPCID()
	h.writeJSON(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "session/set_mode",
		"params":  map[string]any{"sessionId": sid, "mode": "ask"},
	})
	_, errObj, _ := h.readUntilRPCResult(id, 30*time.Second)
	if errObj == nil {
		t.Fatal("session/set_mode should be rejected with method not found")
	}
	if errObj.Code != -32601 {
		t.Fatalf("expected -32601 method not found, got %d %q", errObj.Code, errObj.Message)
	}
	if !strings.Contains(strings.ToLower(errObj.Message), "method not found") {
		t.Fatalf("expected 'method not found' in error message, got %q", errObj.Message)
	}
}

// TestACPIntegration_AutoModeEmitsIntentNotification drives a single
// session/prompt in auto mode against a mock server that classifies as
// SIMPLE_FIX. Asserts session/intent_identified and session/mode_status
// notifications are emitted before the assistant reply.
func TestACPIntegration_AutoModeEmitsIntentNotification(t *testing.T) {
	skipUnlessIntegrationACP(t)
	var chatCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/models" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"mock-openai-model"}]}`))
		case r.URL.Path == "/v1/chat/completions" && r.Method == http.MethodPost:
			n := chatCount.Add(1)
			w.Header().Set("Content-Type", "application/json")
			// First call = supervisor (returns JSON classification).
			// Subsequent calls = the actual agent turn (returns plain text).
			if n == 1 {
				_, _ = io.WriteString(w, `{"id":"mock","object":"chat.completion","model":"mock-openai-model","choices":[{"index":0,"message":{"role":"assistant","content":"{\"category\":\"SIMPLE_FIX\",\"reasoning\":\"tiny tweak\"}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
			} else {
				_, _ = io.WriteString(w, `{"id":"mock","object":"chat.completion","model":"mock-openai-model","choices":[{"index":0,"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":20,"completion_tokens":2,"total_tokens":22}}`)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	stateDir := t.TempDir()
	writeACPIntegrationConfigJSON(t, stateDir, map[string]any{
		"base_url": srv.URL + "/v1",
		"api_key":  "integration-test",
		"model":    "mock-openai-model",
	})
	dir := t.TempDir()
	h := startACPWithEnvMode(t, dir, append(os.Environ(), "CODIENT_STATE_DIR="+stateDir), "auto")
	h.handshake()
	ws, _ := filepath.Abs(dir)
	sid := h.sessionNewWithMode(ws, "auto")

	id := h.nextRPCID()
	h.writeJSON(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "session/prompt",
		"params": map[string]any{
			"sessionId": sid,
			"prompt":    []map[string]any{{"type": "text", "text": "Fix the typo in the README."}},
		},
	})

	deadline := time.Now().Add(2 * time.Minute)
	var sawIntent, sawModeChanged bool
	var intentCategory, intentReason string
	var modeChangedTo string
	for time.Now().Before(deadline) && (!sawIntent || !sawModeChanged) {
		line := h.readLine(30 * time.Second)
		if line == "" {
			continue
		}
		var wrap rpcLine
		if err := json.Unmarshal([]byte(line), &wrap); err != nil {
			continue
		}
		switch wrap.Method {
		case "session/intent_identified":
			var p struct {
				Category  string `json:"category"`
				Reasoning string `json:"reasoning"`
				Fallback  bool   `json:"fallback"`
			}
			if err := json.Unmarshal(wrap.Params, &p); err == nil {
				sawIntent = true
				intentCategory = p.Category
				intentReason = p.Reasoning
			}
		case "session/mode_status":
			var p struct {
				Phase string `json:"phase"`
				Mode  string `json:"mode"`
			}
			if err := json.Unmarshal(wrap.Params, &p); err == nil && p.Phase == "changed" {
				sawModeChanged = true
				modeChangedTo = p.Mode
			}
		}
		if wrap.ID != nil && *wrap.ID == id {
			break
		}
	}
	if !sawIntent {
		t.Fatalf("expected session/intent_identified notification before reply")
	}
	if intentCategory != "SIMPLE_FIX" {
		t.Fatalf("intent category: got %q want SIMPLE_FIX", intentCategory)
	}
	if intentReason == "" {
		t.Fatalf("intent reasoning was empty")
	}
	if !sawModeChanged {
		t.Fatalf("expected session/mode_status phase=changed notification")
	}
	if modeChangedTo != "build" {
		t.Fatalf("orchestrator should map SIMPLE_FIX to build; got %q", modeChangedTo)
	}
	// Drain the final RPC response so the harness shutdown is clean.
	_, errObj, _ := h.readUntilRPCResult(id, 30*time.Second)
	if errObj != nil {
		t.Fatalf("session/prompt error: %d %s", errObj.Code, errObj.Message)
	}
}

// TestACPIntegration_AutoModeComplexTaskHandsOffToBuild is a live LLM
// end-to-end check: a COMPLEX_TASK prompt in auto mode should produce a plan,
// emit session/mode_status phase=plan_ready, then auto-handoff and emit
// phase=changed mode=build with handoff=true. The build phase must call a
// write tool.
func TestACPIntegration_AutoModeComplexTaskHandsOffToBuild(t *testing.T) {
	skipUnlessIntegrationACP(t)
	skipUnlessStrictToolsACP(t)
	if testing.Short() {
		t.Skip("skipping live LLM round in -short mode")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := startACPWithMode(t, dir, "auto")
	h.handshake()
	ws, _ := filepath.Abs(dir)
	sid := h.sessionNewWithMode(ws, "auto")

	user := "Refactor this small project across files: add a new module file, a tests file, and update the README. Provide a structured plan first (Implementation steps, Files to modify, Verification, ending with `Ready to implement`). Then implement the plan creating any required files via tools."
	stop, reply, notes := h.sessionPrompt(sid, user)
	if stop != "" && stop != "end_turn" {
		t.Fatalf("auto complex stop=%q reply=%q", stop, reply)
	}
	var sawIntentComplex, sawPlanReady, sawHandoffChanged, sawWriteTool bool
	for _, raw := range notes {
		var wrap rpcLine
		if err := json.Unmarshal([]byte(raw), &wrap); err != nil {
			continue
		}
		switch wrap.Method {
		case "session/intent_identified":
			var p struct {
				Category string `json:"category"`
			}
			_ = json.Unmarshal(wrap.Params, &p)
			if p.Category == "COMPLEX_TASK" {
				sawIntentComplex = true
			}
		case "session/mode_status":
			var p struct {
				Phase   string `json:"phase"`
				Mode    string `json:"mode"`
				Handoff bool   `json:"handoff"`
			}
			_ = json.Unmarshal(wrap.Params, &p)
			if p.Phase == "plan_ready" {
				sawPlanReady = true
			}
			if p.Phase == "changed" && p.Mode == "build" && p.Handoff {
				sawHandoffChanged = true
			}
		}
		if wrap.Method == "session/update" {
			var u struct {
				Params struct {
					Update struct {
						SessionUpdate string `json:"sessionUpdate"`
						Title         string `json:"title"`
					} `json:"update"`
				} `json:"params"`
			}
			if err := json.Unmarshal([]byte(raw), &u); err != nil {
				continue
			}
			if u.Params.Update.SessionUpdate == "tool_call" {
				head := u.Params.Update.Title
				if i := strings.IndexAny(head, " \t"); i > 0 {
					head = head[:i]
				}
				switch head {
				case "write_file", "str_replace", "patch_file", "insert_lines":
					sawWriteTool = true
				}
			}
		}
	}
	if !sawIntentComplex {
		t.Fatalf("expected supervisor to classify as COMPLEX_TASK; saw notifications=%d", len(notes))
	}
	if !sawPlanReady {
		t.Fatalf("expected session/mode_status phase=plan_ready before handoff")
	}
	if !sawHandoffChanged {
		t.Fatalf("expected session/mode_status phase=changed mode=build handoff=true after plan")
	}
	if !sawWriteTool {
		t.Fatalf("expected at least one write tool call after auto plan->build handoff")
	}
}
