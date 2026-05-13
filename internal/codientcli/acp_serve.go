package codientcli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"

	"codient/internal/acpctx"
	"codient/internal/acpserver"
	"codient/internal/agent"
	"codient/internal/agentlog"
	"codient/internal/assistout"
	"codient/internal/config"
	"codient/internal/errorsink"
	"codient/internal/intent"
	"codient/internal/lspclient"
	"codient/internal/mcpclient"
	"codient/internal/openaiclient"
	"codient/internal/planstore"
	"codient/internal/projectinfo"
	"codient/internal/prompt"
	"codient/internal/repomap"
	"codient/internal/selfupdate"
	"codient/internal/skills"
	"codient/internal/tokentracker"
	"codient/internal/tools"
)

const (
	acpProtocolVersion = 1
	// acpSetModelWarmMax caps how long session/set_model may block on the warmup completion (beyond cfg.MaxCompletionSeconds).
	acpSetModelWarmMax = 10 * time.Minute
	// acpMinCodientUnityPackageSemver: initialize fails if the client sends codientUnityPackageVersion below this.
	// Omitted field = older Codient Unity (allowed). Keep in sync with CodientUnityCompatibility.PackageVersion baseline.
	acpMinCodientUnityPackageSemver = "1.0.0"
)

func validateACPFlags(printMode, repl, stream, ping, listModels, listTools, listProfiles, a2a, update, version bool, nImages int) error {
	var bad []string
	if printMode {
		bad = append(bad, "-print")
	}
	if repl {
		bad = append(bad, "-repl")
	}
	if stream {
		bad = append(bad, "-stream")
	}
	if ping {
		bad = append(bad, "-ping")
	}
	if listModels {
		bad = append(bad, "-list-models")
	}
	if listTools {
		bad = append(bad, "-list-tools")
	}
	if listProfiles {
		bad = append(bad, "-list-profiles")
	}
	if a2a {
		bad = append(bad, "-a2a")
	}
	if update {
		bad = append(bad, "-update")
	}
	if version {
		bad = append(bad, "-version")
	}
	if nImages > 0 {
		bad = append(bad, "-image")
	}
	if len(bad) > 0 {
		return fmt.Errorf("-acp cannot be combined with %s", strings.Join(bad, ", "))
	}
	return nil
}

// runACPServer runs the Agent Client Protocol (ACP) over NDJSON JSON-RPC on stdin/stdout.
func runACPServer(ctx context.Context, cfg *config.Config, client *openaiclient.Client, mcpMgr *mcpclient.Manager, lspMgr *lspclient.Manager, agentLog *agentlog.Logger, maxTurns int, maxCostUSD float64) int {
	if err := cfg.RequireModel(); err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 2
	}

	repoInstr, err := prompt.LoadRepoInstructions(cfg.EffectiveWorkspace())
	if err != nil {
		fmt.Fprintf(os.Stderr, "repo instructions: %v\n", err)
		return 2
	}
	projectCtx := resolveProjectContext(cfg)
	stateDir, err := config.StateDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "codient: state dir: %v\n", err)
	}
	var errorLog *errorsink.Sink
	if !errorsink.Disabled() && stateDir != "" {
		if lg, _, e := errorsink.Open(stateDir); e == nil {
			errorLog = lg
		} else {
			fmt.Fprintf(os.Stderr, "codient: error log: %v\n", e)
		}
	}
	defer func() {
		if errorLog != nil {
			_ = errorLog.Close()
		}
	}()
	mem, err := prompt.LoadMemory(stateDir, cfg.EffectiveWorkspace())
	if err != nil {
		fmt.Fprintf(os.Stderr, "memory: %v\n", err)
	}
	skillsCat := ""
	if stateDir != "" {
		skillsCat, _ = skills.LoadCatalogMarkdown(stateDir, cfg.EffectiveWorkspace())
	}
	var memOpts *tools.MemoryOptions
	if stateDir != "" || cfg.EffectiveWorkspace() != "" {
		memOpts = &tools.MemoryOptions{
			StateDir:      stateDir,
			WorkspaceRoot: cfg.EffectiveWorkspace(),
		}
	}

	var rm *repomap.Map
	if cfg.RepoMapTokens >= 0 {
		ws := cfg.EffectiveWorkspace()
		if ws != "" {
			rm = repomap.New(ws)
			// Do not block the ACP stdio loop on a full workspace scan (Unity-sized trees can take minutes).
			// repomap.Map is safe to read under RLock while Build runs; see repomap.New doc.
			go rm.Build(ctx)
		}
	}

	progressOut := resolveProgressOut(cfg.Progress, strings.TrimSpace(cfg.LogPath) != "")

	tracker := &tokentracker.Tracker{}
	stub := &session{
		cfg:           cfg,
		client:        client,
		progressOut:   progressOut,
		mcpMgr:        mcpMgr,
		lspMgr:        lspMgr,
		repoMap:       rm,
		agentLog:      agentLog,
		errorLog:      errorLog,
		tokenTracker:  tracker,
		acpNoDelegate: true,
		skillsCatalog: skillsCat,
	}
	if len(cfg.ExecAllowlist) > 0 {
		stub.execAllow = tools.NewSessionExecAllow(cfg.ExecAllowlist)
	}

	tr := acpserver.NewTransport()
	stub.acpCallClient = func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		return tr.CallClient(ctx, method, params)
	}
	registryReady := make(chan struct{})
	unityACP := projectinfo.LooksLikeUnityEditorProject(cfg.EffectiveWorkspace())
	srv := &acpServer{
		tr:             tr,
		cfg:            cfg,
		mode:           prompt.ModeAuto,
		client:         client,
		agentLog:       agentLog,
		errorLog:       errorLog,
		version:        Version,
		maxTurns:       maxTurns,
		maxCostUSD:     maxCostUSD,
		repoInstr:      repoInstr,
		projectCtx:     projectCtx,
		memory:         mem,
		memOpts:        memOpts,
		skillsCat:      skillsCat,
		repoMap:        rm,
		unityACPEditor: unityACP,
		stub:           stub,
		initialized:    false,
		sessions:       make(map[string]*acpChatSession),
		progressWriter: progressOut,
		tokenTracker:   tracker,
		registryReady:  registryReady,
	}
	// PostReplyCheck is built once and only attached to runners in Ask mode (per-session).
	srv.postReplyCheck = BuildPostReplyCheckForACP(cfg, client, tracker, prompt.ModeAsk, progressOut)

	// Process session/cancel in the read loop so cancellation reaches the active turn while
	// session/prompt is still blocked on the main handler goroutine.
	tr.ConsumeInbound = func(msg acpserver.WireMsg) bool {
		if msg.Method == "session/cancel" && msg.ID == nil {
			srv.handleNotification(ctx, msg.Method, msg.Params)
			return true
		}
		return false
	}

	stub.execDeniedACP = srv.execPromptDenied

	// Build tool registry off the hot path: stdin must be drained (ReadLoop) while buildRegistry scans the workspace.
	go func() {
		defer close(registryReady)
		reg, sp := srv.buildModeArtifacts(prompt.ModeAuto)
		stub.acpRegistryMu.Lock()
		stub.registry = reg
		stub.systemPrompt = sp
		stub.acpRegistryMu.Unlock()

		needRebuild := false
		if stub.mcpMgr != nil && len(cfg.MCPServers) > 0 {
			mgr := stub.mcpMgr
			go func() {
				mcpCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				warns := mgr.Connect(mcpCtx, cfg.MCPServers)
				for _, w := range warns {
					fmt.Fprintf(os.Stderr, "codient: %s\n", w)
				}
				reg2, sp2 := srv.buildModeArtifacts(prompt.ModeAuto)
				stub.acpRegistryMu.Lock()
				stub.registry = reg2
				stub.systemPrompt = sp2
				stub.acpRegistryMu.Unlock()
			}()
			needRebuild = true
		}
		if stub.lspMgr != nil && len(cfg.LSPServers) > 0 {
			mgr := stub.lspMgr
			go func() {
				lspCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				warns := mgr.Connect(lspCtx, cfg.LSPServers, cfg.EffectiveWorkspace())
				for _, w := range warns {
					fmt.Fprintf(os.Stderr, "codient: %s\n", w)
				}
				reg2, sp2 := srv.buildModeArtifacts(prompt.ModeAuto)
				stub.acpRegistryMu.Lock()
				stub.registry = reg2
				stub.systemPrompt = sp2
				stub.acpRegistryMu.Unlock()
			}()
			needRebuild = true
		}
		_ = needRebuild
	}()

	reqCh := make(chan acpserver.WireMsg, 256)
	go tr.ReadLoop(ctx, reqCh)

	for {
		select {
		case <-ctx.Done():
			return 0
		case msg, ok := <-reqCh:
			if !ok {
				return 0
			}
			if msg.ID != nil && (msg.Result != nil || msg.Error != nil) {
				continue
			}
			if msg.Method != "" && msg.ID == nil {
				srv.handleNotification(ctx, msg.Method, msg.Params)
				continue
			}
			if msg.Method != "" && msg.ID != nil {
				srv.handleRequest(ctx, msg.Method, msg.Params, *msg.ID)
			}
		}
	}
}

type acpServer struct {
	tr             *acpserver.Transport
	cfg            *config.Config
	mode           prompt.Mode
	client         *openaiclient.Client
	agentLog       *agentlog.Logger
	errorLog       *errorsink.Sink
	version        string
	maxTurns       int
	maxCostUSD     float64
	repoInstr      string
	projectCtx     string
	memory         string
	memOpts        *tools.MemoryOptions
	skillsCat      string
	repoMap        *repomap.Map
	unityACPEditor bool
	stub           *session
	// registryReady is closed after the first buildRegistry + systemPrompt (session/* needs a populated stub).
	registryReady    <-chan struct{}
	initialized      bool
	sessions         map[string]*acpChatSession
	mu               sync.Mutex
	progressWriter   io.Writer
	postReplyCheck   func(context.Context, agent.PostReplyCheckInfo) string
	tokenTracker     *tokentracker.Tracker
	permissionSessID string
	permMu           sync.Mutex
}

type acpChatSession struct {
	id            string
	history       []openai.ChatCompletionMessageParamUnion
	systemPrompt  string
	registry      *tools.Registry
	mode          prompt.Mode
	currentPlan   *planstore.Plan
	workspaceRoot string
	modelID       string
	profileID     string // per-session profile override (empty = server default)
	lastPlanReply string // last assistant text in plan mode (for ToT heuristic)
	// lastIntent records the most recent supervisor classification for this
	// session (auto-mode telemetry). Nil until the orchestrator runs a turn.
	lastIntent *intent.Identification
	cancelMu   sync.Mutex
	cancelTurn context.CancelFunc
	// setModelMu serializes session/set_model (including preload) per session.
	setModelMu sync.Mutex
}

// acpLLMWithModel delegates to *openaiclient.Client but reports a session-selected model id.
type acpLLMWithModel struct {
	inner *openaiclient.Client
	id    string
}

func (w acpLLMWithModel) Model() string { return w.id }

func (w acpLLMWithModel) ChatCompletion(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	return w.inner.ChatCompletion(ctx, params)
}

func (w acpLLMWithModel) ChatCompletionStream(ctx context.Context, params openai.ChatCompletionNewParams, streamTo io.Writer, opts ...openaiclient.StreamOption) (*openai.ChatCompletion, error) {
	return w.inner.ChatCompletionStream(ctx, params, streamTo, opts...)
}

func newSessionID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "sess_fallback"
	}
	return "sess_" + hex.EncodeToString(b[:])
}

func (s *acpServer) execPromptDenied(ctx context.Context, deniedKey string, argv []string) tools.ExecPromptChoice {
	if ctx.Err() != nil {
		return tools.ExecPromptDeny
	}
	toolCallID := acpctx.ToolCallID(ctx)
	if toolCallID == "" {
		toolCallID = "exec"
	}
	s.permMu.Lock()
	sid := s.permissionSessID
	s.permMu.Unlock()
	if sid == "" {
		return tools.ExecPromptDeny
	}
	raw, err := s.tr.CallClient(ctx, "session/request_permission", map[string]any{
		"sessionId": sid,
		"toolCall":  map[string]any{"toolCallId": toolCallID},
		"options": []map[string]any{
			{"optionId": "codient-allow-once", "name": "Allow once", "kind": "allow_once"},
			{"optionId": "codient-reject-once", "name": "Reject", "kind": "reject_once"},
		},
	})
	if err != nil || len(raw) == 0 {
		return tools.ExecPromptDeny
	}
	var envelope struct {
		Outcome struct {
			Outcome  string `json:"outcome"`
			OptionID string `json:"optionId"`
		} `json:"outcome"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return tools.ExecPromptDeny
	}
	switch envelope.Outcome.Outcome {
	case "cancelled":
		return tools.ExecPromptDeny
	case "selected":
		switch envelope.Outcome.OptionID {
		case "codient-allow-once":
			return tools.ExecPromptAllowSession
		case "codient-reject-once":
			return tools.ExecPromptDeny
		default:
			return tools.ExecPromptDeny
		}
	default:
		return tools.ExecPromptDeny
	}
}

func (s *acpServer) handleNotification(ctx context.Context, method string, params json.RawMessage) {
	switch method {
	case "session/cancel":
		var p struct {
			SessionID string `json:"sessionId"`
		}
		_ = json.Unmarshal(params, &p)
		s.mu.Lock()
		sess := s.sessions[p.SessionID]
		s.mu.Unlock()
		if sess == nil {
			return
		}
		sess.cancelActiveTurn()
	default:
		_ = ctx
	}
}

func (s *acpServer) waitACPRegistryReady(ctx context.Context) error {
	select {
	case <-s.registryReady:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *acpServer) handleRequest(ctx context.Context, method string, params json.RawMessage, id int) {
	switch method {
	case "initialize":
		s.handleInitialize(ctx, params, id)
	case "agent/list_models":
		s.handleAgentListModels(ctx, id)
	case "agent/list_profiles":
		s.handleAgentListProfiles(ctx, id)
	case "session/new":
		s.handleSessionNew(ctx, params, id)
	case "session/set_model":
		s.handleSessionSetModel(ctx, params, id)
	case "session/set_profile":
		s.handleSessionSetProfile(ctx, params, id)
	case "session/prompt":
		s.handleSessionPrompt(ctx, params, id)
	default:
		_ = s.tr.WriteError(id, -32601, "method not found: "+method)
	}
}

func (s *acpServer) handleInitialize(_ context.Context, params json.RawMessage, id int) {
	var p struct {
		ProtocolVersion int `json:"protocolVersion"`
		ClientInfo      *struct {
			CodientUnityPackageVersion string `json:"codientUnityPackageVersion"`
		} `json:"clientInfo"`
	}
	_ = json.Unmarshal(params, &p)
	if p.ProtocolVersion != acpProtocolVersion {
		_ = s.tr.WriteError(id, -32602, fmt.Sprintf("unsupported protocol version %d (want %d)", p.ProtocolVersion, acpProtocolVersion))
		return
	}
	if p.ClientInfo != nil {
		pkg := strings.TrimSpace(p.ClientInfo.CodientUnityPackageVersion)
		if pkg != "" {
			if !selfupdate.ValidSemver(pkg) {
				_ = s.tr.WriteError(id, -32602, fmt.Sprintf("invalid codientUnityPackageVersion %q (need major.minor.patch semver)", pkg))
				return
			}
			if !selfupdate.SemverAtLeast(pkg, acpMinCodientUnityPackageSemver) {
				_ = s.tr.WriteError(id, -32602, fmt.Sprintf("Codient Unity package %s is too old for this codient (need ≥ %s). Upgrade the Codient Unity package.", pkg, acpMinCodientUnityPackageSemver))
				return
			}
		}
	}
	s.initialized = true
	defaultModel := strings.TrimSpace(s.client.Model())

	profileNames := config.ProfileNamesList(s.cfg.Profiles)
	activeProfile := s.cfg.ActiveProfile

	_ = s.tr.WriteResult(id, map[string]any{
		"protocolVersion":  acpProtocolVersion,
		"defaultChatModel": defaultModel,
		"agentCapabilities": map[string]any{
			"loadSession": false,
			"profiles":    len(profileNames) > 0,
			"promptCapabilities": map[string]any{
				"image":           false,
				"audio":           false,
				"embeddedContext": true,
			},
			"mcpCapabilities": map[string]any{
				"http": false,
				"sse":  false,
			},
		},
		"agentInfo": map[string]any{
			"name":    "codient",
			"title":   "Codient",
			"version": s.version,
		},
		"activeProfile": activeProfile,
		"profiles":      profileNames,
		"authMethods":   []any{},
	})
}

// handleAgentListModels returns model ids from the configured OpenAI-compatible GET /v1/models endpoint.
func (s *acpServer) handleAgentListModels(ctx context.Context, id int) {
	listCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	ids, err := s.client.ListModels(listCtx)
	if err != nil {
		_ = s.tr.WriteError(id, -32603, err.Error())
		return
	}
	sort.Strings(ids)
	out := make([]map[string]string, 0, len(ids))
	for _, m := range ids {
		out = append(out, map[string]string{"id": m})
	}
	_ = s.tr.WriteResult(id, map[string]any{"models": out})
}

func (s *acpServer) handleSessionNew(ctx context.Context, params json.RawMessage, id int) {
	if !s.initialized {
		_ = s.tr.WriteError(id, -32002, "not initialized")
		return
	}
	if err := s.waitACPRegistryReady(ctx); err != nil {
		_ = s.tr.WriteError(id, -32603, err.Error())
		return
	}
	var p struct {
		Cwd     string `json:"cwd"`
		Model   string `json:"model"`
		Profile string `json:"profile"`
		// Mode is accepted but ignored: every session is auto-mode and the
		// orchestrator picks an internal mode per session/prompt. Older clients
		// that still send "mode" keep working.
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		_ = s.tr.WriteError(id, -32602, "invalid params")
		return
	}
	// Validate profile param if provided.
	profName := strings.TrimSpace(p.Profile)
	if profName != "" {
		if !config.ProfileNameRe.MatchString(profName) {
			_ = s.tr.WriteError(id, -32602, fmt.Sprintf("invalid profile name %q", profName))
			return
		}
		if _, ok := s.cfg.Profiles[profName]; !ok {
			_ = s.tr.WriteError(id, -32602, fmt.Sprintf("unknown profile %q", profName))
			return
		}
	}
	ws := filepath.Clean(s.cfg.EffectiveWorkspace())
	cwd, err := filepath.Abs(strings.TrimSpace(p.Cwd))
	if err != nil {
		_ = s.tr.WriteError(id, -32602, "invalid cwd")
		return
	}
	cwd = filepath.Clean(cwd)
	if !strings.EqualFold(ws, cwd) {
		_ = s.tr.WriteError(id, -32602, fmt.Sprintf("cwd %q must match workspace %q", cwd, ws))
		return
	}
	sid := newSessionID()
	reg, sp := s.modeArtifactsFor(prompt.ModeAuto)
	sess := &acpChatSession{
		id:            sid,
		history:       nil,
		systemPrompt:  sp,
		registry:      reg,
		mode:          prompt.ModeAuto,
		workspaceRoot: cwd,
		modelID:       strings.TrimSpace(p.Model),
		profileID:     profName,
	}
	s.mu.Lock()
	s.sessions[sid] = sess
	s.mu.Unlock()
	_ = s.tr.WriteResult(id, map[string]any{"sessionId": sid, "mode": string(prompt.ModeAuto), "profile": profName})
}

// modeArtifactsFor returns the registry and system prompt to use for the given mode.
// The default-mode artifacts are cached on stub (built off the hot path); other modes
// are built on demand. Callers should treat the returned registry as immutable.
func (s *acpServer) modeArtifactsFor(mode prompt.Mode) (*tools.Registry, string) {
	if mode == s.mode {
		s.stub.acpRegistryMu.RLock()
		reg := s.stub.registry
		sp := s.stub.systemPrompt
		s.stub.acpRegistryMu.RUnlock()
		if reg != nil && sp != "" {
			return reg, sp
		}
	}
	return s.buildModeArtifacts(mode)
}

// buildModeArtifacts constructs a fresh registry + system prompt for the requested mode,
// using the server-level project context, repo map, skills, and Unity ACP heuristic.
func (s *acpServer) buildModeArtifacts(mode prompt.Mode) (*tools.Registry, string) {
	reg := buildRegistry(s.cfg, mode, s.stub, s.memOpts)
	sp := buildAgentSystemPromptEx(s.cfg, reg, mode, "", s.repoInstr, s.projectCtx, s.memory, s.skillsCat, s.repoMap, s.unityACPEditor)
	return reg, sp
}

// handleSessionSetModel updates the per-session model id without clearing history (Codient Unity model picker).
func (s *acpServer) handleSessionSetModel(ctx context.Context, params json.RawMessage, id int) {
	if !s.initialized {
		_ = s.tr.WriteError(id, -32002, "not initialized")
		return
	}
	if err := s.waitACPRegistryReady(ctx); err != nil {
		_ = s.tr.WriteError(id, -32603, err.Error())
		return
	}
	var p struct {
		SessionID string `json:"sessionId"`
		Model     string `json:"model"`
		Preload   *bool  `json:"preload"`
	}
	if err := json.Unmarshal(params, &p); err != nil || strings.TrimSpace(p.SessionID) == "" {
		_ = s.tr.WriteError(id, -32602, "invalid session/set_model params")
		return
	}
	s.mu.Lock()
	sess := s.sessions[p.SessionID]
	s.mu.Unlock()
	if sess == nil {
		_ = s.tr.WriteError(id, -32001, "unknown session")
		return
	}

	preload := s.cfg.AcpPreloadModelOnSetModel
	if p.Preload != nil {
		preload = *p.Preload
	}

	sess.setModelMu.Lock()
	defer sess.setModelMu.Unlock()

	mid := strings.TrimSpace(p.Model)
	sess.cancelMu.Lock()
	if sess.cancelTurn != nil {
		sess.cancelMu.Unlock()
		_ = s.tr.WriteError(id, -32603, "session_busy")
		return
	}
	prev := strings.TrimSpace(sess.modelID)
	sess.cancelMu.Unlock()

	prevEff := s.acpEffectiveModelID(prev)
	newEff := s.acpEffectiveModelID(mid)
	switching := !strings.EqualFold(prevEff, newEff)

	// Ollama keeps the previous model resident until evicted; unload it before warming the new one.
	if switching && prevEff != "" {
		_ = s.tr.SendNotification("session/model_status", map[string]any{
			"sessionId": p.SessionID,
			"phase":     "unloading",
			"message":   "Unloading previous model from the inference server…",
		})
		_ = s.client.TryOllamaUnloadModel(ctx, prevEff)
	}

	sess.cancelMu.Lock()
	sess.modelID = mid
	sess.cancelMu.Unlock()

	if preload && switching {
		llm := s.acpSessionChatClient(sess)
		if err := s.acpWarmSessionModel(ctx, p.SessionID, llm); err != nil {
			sess.cancelMu.Lock()
			sess.modelID = prev
			sess.cancelMu.Unlock()
			_ = s.tr.SendNotification("session/model_status", map[string]any{
				"sessionId": p.SessionID,
				"phase":     "error",
				"message":   err.Error(),
			})
			_ = s.tr.WriteError(id, -32603, "session/set_model preload: "+err.Error())
			return
		}
	}

	_ = s.tr.WriteResult(id, map[string]any{"model": mid})
}

func (s *acpServer) acpEffectiveModelID(sessionModelTrimmed string) string {
	if sessionModelTrimmed != "" {
		return sessionModelTrimmed
	}
	return strings.TrimSpace(s.client.Model())
}

func (s *acpServer) acpSessionChatClient(sess *acpChatSession) agent.ChatClient {
	sess.cancelMu.Lock()
	mid := strings.TrimSpace(sess.modelID)
	sess.cancelMu.Unlock()
	if mid != "" {
		return acpLLMWithModel{inner: s.client, id: mid}
	}
	return s.client
}

func (s *acpServer) acpWarmSessionModel(ctx context.Context, sessionID string, llm agent.ChatClient) error {
	_ = s.tr.SendNotification("session/model_status", map[string]any{
		"sessionId": sessionID,
		"phase":     "loading",
		"message":   "Contacting inference server and loading model…",
	})
	warmTimeout := time.Duration(s.cfg.MaxCompletionSeconds) * time.Second
	if warmTimeout < 30*time.Second {
		warmTimeout = 30 * time.Second
	}
	if warmTimeout > acpSetModelWarmMax {
		warmTimeout = acpSetModelWarmMax
	}
	warmCtx, cancel := context.WithTimeout(ctx, warmTimeout)
	defer cancel()
	params := openai.ChatCompletionNewParams{
		Model:    shared.ChatModel(llm.Model()),
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage(".")},
	}
	params.MaxCompletionTokens = openai.Int(1)
	_, err := llm.ChatCompletion(warmCtx, params)
	if err != nil {
		return err
	}
	_ = s.tr.SendNotification("session/model_status", map[string]any{
		"sessionId": sessionID,
		"phase":     "ready",
		"message":   "",
	})
	return nil
}

func (s *acpServer) handleSessionPrompt(ctx context.Context, params json.RawMessage, id int) {
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			if s.errorLog != nil {
				s.errorLog.LogPanic(r, stack)
			}
			msg := fmt.Sprintf("internal error: panic: %v", r)
			if s.errorLog != nil {
				if p := s.errorLog.Path(); p != "" {
					msg += " (details in " + p + ")"
				}
			}
			_ = s.tr.WriteError(id, -32603, msg)
		}
	}()
	if !s.initialized {
		_ = s.tr.WriteError(id, -32002, "not initialized")
		return
	}
	if err := s.waitACPRegistryReady(ctx); err != nil {
		_ = s.tr.WriteError(id, -32603, err.Error())
		return
	}
	var p struct {
		SessionID string          `json:"sessionId"`
		Prompt    json.RawMessage `json:"prompt"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.SessionID == "" {
		_ = s.tr.WriteError(id, -32602, "invalid session/prompt params")
		return
	}
	s.mu.Lock()
	sess := s.sessions[p.SessionID]
	s.mu.Unlock()
	if sess == nil {
		_ = s.tr.WriteError(id, -32001, "unknown session")
		return
	}

	userText, err := acpExtractPromptText(p.Prompt)
	if err != nil {
		_ = s.tr.WriteError(id, -32602, err.Error())
		return
	}

	promptCtx, cancel := context.WithCancel(ctx)
	sess.setCancel(cancel)
	defer sess.clearCancel()

	s.permMu.Lock()
	s.permissionSessID = p.SessionID
	s.permMu.Unlock()
	defer func() {
		s.permMu.Lock()
		if s.permissionSessID == p.SessionID {
			s.permissionSessID = ""
		}
		s.permMu.Unlock()
	}()

	cw := &acpChunkWriter{tr: s.tr, sessionID: p.SessionID}

	userMsg := openai.UserMessage(userText)

	defer func() {
		// Restore the auto sentinel so the next session/prompt re-classifies.
		sess.cancelMu.Lock()
		sess.mode = prompt.ModeAuto
		sess.cancelMu.Unlock()
	}()

	reply, runErr := s.orchestrateACPTurn(promptCtx, sess, p.SessionID, userText, userMsg, cw)

	if errors.Is(runErr, context.Canceled) {
		_ = s.tr.WriteResult(id, map[string]any{"stopReason": "cancelled"})
		return
	}
	if errors.Is(runErr, agent.ErrMaxTurns) {
		_ = s.tr.WriteResult(id, map[string]any{"stopReason": "max_turn_requests"})
		return
	}
	if errors.Is(runErr, agent.ErrMaxCost) {
		_ = s.tr.WriteResult(id, map[string]any{"stopReason": "max_tokens"})
		return
	}
	if runErr != nil {
		if s.errorLog != nil {
			s.errorLog.LogError(fmt.Sprintf("acp:session/prompt session_id=%s", p.SessionID), runErr)
		}
		_ = s.tr.WriteError(id, -32603, runErr.Error())
		return
	}
	if strings.TrimSpace(reply) != "" && cw.chunks == 0 {
		_ = s.tr.SendNotification("session/update", map[string]any{
			"sessionId": p.SessionID,
			"update": map[string]any{
				"sessionUpdate": "agent_message_chunk",
				"content": map[string]any{
					"type": "text",
					"text": reply,
				},
			},
		})
	}
	_ = s.tr.WriteResult(id, map[string]any{"stopReason": "end_turn", "reply": reply})
}

// runACPTurn runs one logical turn against sess (with PlanTot fallback when in
// plan mode) and returns the assistant reply. Updates sess.history,
// sess.lastPlanReply, and sess.currentPlan as side-effects so subsequent calls
// see the latest state. userText is used to seed plan parsing; pass "" for
// synthetic continuations (e.g. the plan->build handoff message).
func (s *acpServer) runACPTurn(ctx context.Context, sess *acpChatSession, userMsg openai.ChatCompletionMessageParamUnion, userText string, cw *acpChunkWriter) (string, error) {
	r := s.newACPRunnerLocked(sess)
	var reply string
	var newHist []openai.ChatCompletionMessageParamUnion
	var runErr error
	usePlanTot := sess.mode == prompt.ModePlan && s.cfg.PlanTot &&
		(len(sess.history) == 0 || assistout.ReplySignalsPlanWait(sess.lastPlanReply))
	if usePlanTot {
		totClient := agent.NewPlanTotOpenAIClient(s.cfg)
		var used bool
		reply, newHist, _, used, runErr = agent.RunPlanModeTot(ctx, r, totClient, sess.systemPrompt, sess.history, userMsg, cw)
		if runErr == nil && !used {
			reply, newHist, _, runErr = r.RunConversation(ctx, sess.systemPrompt, sess.history, userMsg, cw)
		}
	} else {
		reply, newHist, _, runErr = r.RunConversation(ctx, sess.systemPrompt, sess.history, userMsg, cw)
	}
	if runErr != nil {
		return reply, runErr
	}
	sess.history = newHist
	if sess.mode == prompt.ModePlan {
		designText := assistout.PrepareAssistantText(reply, true)
		sess.lastPlanReply = designText
		// Keep sess.currentPlan in sync so the orchestrator's auto-build phase
		// can inject a structured handoff. Skip parsing for synthetic
		// continuations (userText empty) — they are implementation directives,
		// not new plan requests.
		if userText != "" {
			if parsed := planstore.ParseFromMarkdown(designText, userText); parsed != nil && len(parsed.Steps) > 0 {
				parsed.SessionID = sess.id
				parsed.Workspace = sess.workspaceRoot
				if sess.currentPlan == nil {
					parsed.Revision = 1
				} else {
					parsed.Revision = sess.currentPlan.Revision + 1
				}
				sess.currentPlan = parsed
			}
		}
	}
	return reply, nil
}

// orchestrateACPTurn classifies userText with the low-tier supervisor, swaps
// sess artifacts to the chosen target mode, runs the turn, and (for
// COMPLEX_TASK whose plan triggers handoff) chains a build phase. The
// build-mode transition is performed FIRST so the handoff user message is
// built against the build-mode tool registry, then runACPTurn drives the
// implementation turn with that synthetic message. Notifications surface each
// transition so Codient Unity can render the orchestrator activity.
func (s *acpServer) orchestrateACPTurn(ctx context.Context, sess *acpChatSession, sessionID, userText string, userMsg openai.ChatCompletionMessageParamUnion, cw *acpChunkWriter) (string, error) {
	id := s.classifyACPIntent(ctx, sess, userText)
	sess.lastIntent = &id
	s.emitIntentNotification(sessionID, id)

	target := mapCategoryToMode(id.Category)
	s.applyACPMode(sess, target)
	s.emitModeStatusChanged(sessionID, target, false)

	reply, err := s.runACPTurn(ctx, sess, userMsg, userText, cw)
	if err != nil {
		return reply, err
	}
	if id.Category != intent.CategoryComplexTask || sess.mode != prompt.ModePlan {
		return reply, nil
	}
	if !planHandoffApplies(sess.currentPlan, sess.lastPlanReply) {
		return reply, nil
	}
	ready := evaluatePlanReadiness(ctx, openaiclient.NewForTier(s.cfg, config.TierLow), s.tokenTracker, sess.currentPlan, sess.lastPlanReply)
	if !ready.Ready {
		return reply, nil
	}

	// Plan ready — emit the plan_ready notification (so editors can render the
	// pause point), then auto-handoff and run the build phase. The orchestrator
	// always chains plan->build for COMPLEX_TASK; clients that want to inspect
	// or edit the plan first can do so between the plan_ready and the next
	// session/prompt notification.
	_ = s.tr.SendNotification("session/mode_status", map[string]any{
		"sessionId": sessionID,
		"phase":     "plan_ready",
		"mode":      string(prompt.ModePlan),
		"handoff":   false,
	})

	if sess.currentPlan != nil {
		sess.currentPlan.Phase = planstore.PhaseApproved
		if sess.currentPlan.Approval == nil {
			recordApproval(sess.currentPlan, "approve", "approved by orchestrator (auto)")
		}
	}
	// Swap to build first so the handoff message references the build-mode
	// tool registry, not the plan-mode one.
	s.applyACPMode(sess, prompt.ModeBuild)
	s.emitModeStatusChanged(sessionID, prompt.ModeBuild, true)
	handoffText := buildPlanHandoffMessage(sess.currentPlan, sess.lastPlanReply, sess.registry.Names())
	return s.runACPTurn(ctx, sess, openai.UserMessage(handoffText), "", cw)
}

// applyACPMode swaps sess.mode and the resolved registry / system prompt for
// the target mode without persisting the change. The orchestrator wraps each
// turn with this helper so the next turn re-classifies (sess.mode is restored
// to ModeAuto by handleSessionPrompt's deferred restore).
func (s *acpServer) applyACPMode(sess *acpChatSession, target prompt.Mode) {
	reg, sp := s.modeArtifactsFor(target)
	sess.cancelMu.Lock()
	sess.mode = target
	sess.registry = reg
	sess.systemPrompt = sp
	sess.cancelMu.Unlock()
}

// classifyACPIntent runs the supervisor on userText using the low-tier client.
// Errors are folded into the returned Identification (fallback path = QUERY).
func (s *acpServer) classifyACPIntent(ctx context.Context, sess *acpChatSession, userText string) intent.Identification {
	cli := openaiclient.NewForTier(s.cfg, config.TierLow)
	var lastReply, planSummary, lastMode string
	if sess != nil {
		lastReply = sess.lastPlanReply
		var phase planstore.Phase
		if sess.currentPlan != nil {
			phase = sess.currentPlan.Phase
		}
		planSummary = summarizePlanForIntent(sess.currentPlan, phase)
		lastMode = string(sess.mode)
	}
	id, err := intent.IdentifyIntent(ctx, cli, userText, intent.Options{
		Tracker:             s.tokenTracker,
		MaxCompletionTokens: s.cfg.LowReasoning.MaxCompletionTokens,
		DisableHeuristic:    s.cfg.DisableIntentHeuristic,
		LastAssistantReply:  lastReply,
		ActivePlanSummary:   planSummary,
		LastResolvedMode:    lastMode,
	})
	if err != nil && s.errorLog != nil {
		s.errorLog.LogError("acp:intent_identify", err)
	}
	return id
}

// emitIntentNotification fires the session/intent_identified notification so
// Codient Unity can show the orchestrator's decision in its transcript.
// The `source` field is one of "supervisor" (LLM-classified), "heuristic"
// (pre-LLM fast-path), or "heuristic-fallback" (post-LLM-failure safety
// net). Older clients that ignore the field still get the same
// category / reasoning / fallback payload.
func (s *acpServer) emitIntentNotification(sessionID string, id intent.Identification) {
	source := id.Source
	if source == "" {
		// Older code paths (and the empty-prompt / nil-client guard) leave
		// Source unset. Treat as supervisor for the wire format so clients
		// always see a populated source string.
		source = intent.SourceSupervisor
	}
	_ = s.tr.SendNotification("session/intent_identified", map[string]any{
		"sessionId": sessionID,
		"category":  string(id.Category),
		"reasoning": id.Reasoning,
		"fallback":  id.Fallback,
		"source":    string(source),
	})
}

// emitModeStatusChanged notifies the client that the orchestrator switched
// the active mode for the upcoming turn (auto -> resolved). handoff is true
// when the change carried a plan->build implementation directive.
func (s *acpServer) emitModeStatusChanged(sessionID string, target prompt.Mode, handoff bool) {
	_ = s.tr.SendNotification("session/mode_status", map[string]any{
		"sessionId": sessionID,
		"phase":     "changed",
		"mode":      string(target),
		"handoff":   handoff,
	})
}

func (sess *acpChatSession) setCancel(c context.CancelFunc) {
	sess.cancelMu.Lock()
	defer sess.cancelMu.Unlock()
	sess.cancelTurn = c
}

func (sess *acpChatSession) clearCancel() {
	sess.cancelMu.Lock()
	defer sess.cancelMu.Unlock()
	sess.cancelTurn = nil
}

func (sess *acpChatSession) cancelActiveTurn() {
	sess.cancelMu.Lock()
	c := sess.cancelTurn
	sess.cancelMu.Unlock()
	if c != nil {
		c()
	}
}

// handleAgentListProfiles returns the configured profile names and active profile.
func (s *acpServer) handleAgentListProfiles(_ context.Context, id int) {
	names := config.ProfileNamesList(s.cfg.Profiles)
	type profileInfo struct {
		Name  string `json:"name"`
		Model string `json:"model,omitempty"`
	}
	out := make([]profileInfo, 0, len(names))
	for _, n := range names {
		model := ""
		if p, ok := s.cfg.Profiles[n]; ok && p.Model != nil {
			model = *p.Model
		}
		out = append(out, profileInfo{Name: n, Model: model})
	}
	_ = s.tr.WriteResult(id, map[string]any{
		"active":   s.cfg.ActiveProfile,
		"profiles": out,
	})
}

// handleSessionSetProfile switches the per-session profile and emits a notification.
func (s *acpServer) handleSessionSetProfile(ctx context.Context, params json.RawMessage, id int) {
	if !s.initialized {
		_ = s.tr.WriteError(id, -32002, "not initialized")
		return
	}
	if err := s.waitACPRegistryReady(ctx); err != nil {
		_ = s.tr.WriteError(id, -32603, err.Error())
		return
	}
	var p struct {
		SessionID string `json:"sessionId"`
		Profile   string `json:"profile"`
	}
	if err := json.Unmarshal(params, &p); err != nil || strings.TrimSpace(p.SessionID) == "" {
		_ = s.tr.WriteError(id, -32602, "invalid session/set_profile params")
		return
	}
	s.mu.Lock()
	sess := s.sessions[p.SessionID]
	s.mu.Unlock()
	if sess == nil {
		_ = s.tr.WriteError(id, -32001, "unknown session")
		return
	}
	sess.cancelMu.Lock()
	if sess.cancelTurn != nil {
		sess.cancelMu.Unlock()
		_ = s.tr.WriteError(id, -32603, "session_busy")
		return
	}
	sess.cancelMu.Unlock()

	profName := strings.TrimSpace(p.Profile)
	if profName != "" {
		if !config.ProfileNameRe.MatchString(profName) {
			_ = s.tr.WriteError(id, -32602, fmt.Sprintf("invalid profile name %q", profName))
			return
		}
		if _, ok := s.cfg.Profiles[profName]; !ok {
			_ = s.tr.WriteError(id, -32602, fmt.Sprintf("unknown profile %q", profName))
			return
		}
	}

	sess.cancelMu.Lock()
	sess.profileID = profName
	sess.cancelMu.Unlock()

	resolvedModel := s.acpEffectiveModelID(strings.TrimSpace(sess.modelID))

	_ = s.tr.WriteResult(id, map[string]any{"profile": profName})
	_ = s.tr.SendNotification("session/profile_changed", map[string]any{
		"sessionId":     p.SessionID,
		"profile":       profName,
		"resolvedModel": resolvedModel,
	})
}

func acpExtractPromptText(prompt json.RawMessage) (string, error) {
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(prompt, &blocks); err != nil {
		return "", fmt.Errorf("prompt: %w", err)
	}
	var sb strings.Builder
	for _, b := range blocks {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	if strings.TrimSpace(sb.String()) == "" {
		return "", fmt.Errorf("prompt: no text content")
	}
	return sb.String(), nil
}

// acpStructuredPatchPreview returns optional JSON for IDE clients (Unity transcript); unknown keys are safe to ignore.
func acpStructuredPatchPreview(name string, args json.RawMessage) map[string]any {
	if len(args) == 0 {
		return nil
	}
	switch name {
	case "str_replace":
		var p struct {
			Path      string `json:"path"`
			OldString string `json:"old_string"`
			NewString string `json:"new_string"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return nil
		}
		return map[string]any{
			"type":       "str_replace",
			"path":       p.Path,
			"old_string": p.OldString,
			"new_string": p.NewString,
		}
	case "patch_file":
		var p struct {
			Path string `json:"path"`
			Diff string `json:"diff"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return nil
		}
		return map[string]any{
			"type": "patch_file",
			"path": p.Path,
			"diff": p.Diff,
		}
	default:
		return nil
	}
}

type acpChunkWriter struct {
	tr        *acpserver.Transport
	sessionID string
	chunks    int
}

func (w *acpChunkWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	text := string(p)
	if err := w.tr.SendNotification("session/update", map[string]any{
		"sessionId": w.sessionID,
		"update": map[string]any{
			"sessionUpdate": "agent_message_chunk",
			"content": map[string]any{
				"type": "text",
				"text": text,
			},
		},
	}); err != nil {
		return 0, err
	}
	w.chunks++
	return len(p), nil
}

func (s *acpServer) newACPRunnerLocked(sess *acpChatSession) *agent.Runner {
	reg := sess.registry
	if reg == nil {
		s.stub.acpRegistryMu.RLock()
		reg = s.stub.registry
		s.stub.acpRegistryMu.RUnlock()
	}
	llm := s.acpSessionChatClient(sess)
	r := &agent.Runner{
		LLM:           llm,
		Cfg:           s.cfg,
		Tools:         reg,
		Log:           s.agentLog,
		ErrorLog:      s.errorLog,
		Progress:      s.progressWriter,
		ProgressPlain: s.cfg.Plain,
		ProgressMode:  string(sess.mode),
		Tracker:       s.tokenTracker,
		MaxTurns:      s.maxTurns,
		MaxCostUSD:    s.maxCostUSD,
	}
	if sess.mode == prompt.ModeAsk {
		r.PostReplyCheck = s.postReplyCheck
	}
	if s.maxCostUSD > 0 {
		r.EstimateSessionCost = func(u tokentracker.Usage) (float64, bool) {
			return s.stub.estimateCostForUsage(u)
		}
	}
	if sess.mode == prompt.ModeBuild {
		steps := buildAutoCheckSteps(s.cfg)
		if len(steps) > 0 {
			sec := autoCheckTimeoutSec(s.cfg)
			r.AutoCheck = makeAutoCheckSequenceWithConfig(s.cfg, s.cfg.EffectiveWorkspace(), steps, time.Duration(sec)*time.Second, s.cfg.ExecMaxOutputBytes, s.progressWriter)
		}
		r.AutoCheckMaxFixes = s.cfg.AutoCheckFixMaxRetries
		r.AutoCheckStopOnNoProgress = s.cfg.AutoCheckFixStopOnNoProgress
		if s.cfg.BuildSelfCritique {
			r.PostReplyCheck = makeBuildSelfCritique()
		}
	}
	sid := sess.id
	r.OnToolBefore = func(ctx context.Context, toolCallID, name string, args json.RawMessage) {
		_ = ctx
		_ = s.tr.SendNotification("session/update", map[string]any{
			"sessionId": sid,
			"update": map[string]any{
				"sessionUpdate": "tool_call",
				"toolCallId":    toolCallID,
				"title":         acpToolTitle(name, args),
				"kind":          acpToolKind(name),
				"status":        "pending",
			},
		})
	}
	r.OnIntent = func(text string) {
		intent := strings.TrimSpace(text)
		if intent == "" {
			return
		}
		intent = acpTruncateRunes(intent, 800)
		_ = s.tr.SendNotification("session/update", map[string]any{
			"sessionId": sid,
			"update": map[string]any{
				"sessionUpdate": "plan",
				"entries": []map[string]any{
					{"content": intent},
				},
			},
		})
		// Fallback for UIs that don't currently render plan entries:
		// mirror intent as one assistant text chunk.
		_ = s.tr.SendNotification("session/update", map[string]any{
			"sessionId": sid,
			"update": map[string]any{
				"sessionUpdate": "agent_message_chunk",
				"content": map[string]any{
					"type": "text",
					"text": intent + "\n\n",
				},
			},
		})
	}
	r.OnToolAfter = func(ctx context.Context, toolCallID, name string, args json.RawMessage, display string, err error) {
		_ = ctx
		st := "completed"
		if err != nil || strings.HasPrefix(display, "error:") {
			st = "failed"
		}
		display = acpToolDisplayForUpdate(name, display, st == "failed")
		update := map[string]any{
			"sessionUpdate": "tool_call_update",
			"toolCallId":    toolCallID,
			"status":        st,
		}
		if preview := acpStructuredPatchPreview(name, args); preview != nil {
			update["structuredPatch"] = preview
		}
		if st == "completed" && display != "" && !strings.HasPrefix(display, "error:") {
			update["content"] = []map[string]any{
				{
					"type": "content",
					"content": map[string]any{
						"type": "text",
						"text": display,
					},
				},
			}
		} else if st == "failed" {
			update["content"] = []map[string]any{
				{
					"type": "content",
					"content": map[string]any{
						"type": "text",
						"text": display,
					},
				},
			}
		}
		_ = s.tr.SendNotification("session/update", map[string]any{
			"sessionId": sid,
			"update":    update,
		})
	}
	return r
}

func acpToolKind(name string) string {
	switch name {
	case "read_file", "grep", "glob", "list_dir", "semantic_search", "read_lines":
		return "read"
	case "write_file", "str_replace", "patch_file", "insert_lines", "remove_path", "move_path", "copy_path":
		return "edit"
	case "run_command", "run_shell":
		return "execute"
	case "fetch_url", "web_search":
		return "fetch"
	case "unity_apply_actions":
		return "edit"
	default:
		if strings.HasPrefix(name, "unity_") {
			return "read"
		}
		return "other"
	}
}

func acpToolTitle(name string, args json.RawMessage) string {
	d := acpToolDescriptor(name, args)
	line := acpToolCliTitle(name, d)
	if len(line) <= 160 {
		return line
	}
	return line[:157] + "..."
}

func acpToolCliTitle(name, descriptor string) string {
	d := strings.TrimSpace(descriptor)
	switch name {
	case "read_file", "read_lines":
		if d == "" {
			return "reading file"
		}
		return "reading " + d
	case "list_dir":
		if d == "" {
			return "listing workspace"
		}
		return "listing " + d
	case "glob", "glob_files", "search_files":
		if d == "" {
			return "searching files"
		}
		return "searching files " + d
	case "grep":
		if d == "" {
			return "searching code"
		}
		return "searching code " + d
	case "path_stat":
		if d == "" {
			return "checking path"
		}
		return "checking " + d
	case "run_command", "run_shell":
		if d == "" {
			return "running command"
		}
		return "running " + d
	case "fetch_url":
		if d == "" {
			return "fetching url"
		}
		return "fetching " + d
	case "web_search":
		if d == "" {
			return "searching web"
		}
		return "searching web " + d
	default:
		if d == "" {
			return name
		}
		return name + " " + d
	}
}

func acpToolDisplayForUpdate(name, display string, isFailed bool) string {
	d := strings.TrimSpace(display)
	if d == "" {
		return ""
	}
	// Keep error payloads visible for debugging, but still bounded.
	if isFailed {
		return acpTruncateRunes(d, 1200)
	}
	if strings.HasPrefix(name, "unity_") {
		return acpTruncateRunes(d, 1400)
	}
	switch name {
	case "read_file", "read_lines":
		return "(file content hidden)"
	case "grep":
		return acpTruncateRunes(d, 800)
	case "list_dir", "glob", "search_files", "semantic_search", "repo_map":
		return acpTruncateRunes(d, 1000)
	default:
		return acpTruncateRunes(d, 1400)
	}
}

func acpTruncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	rs := []rune(s)
	if len(rs) <= max {
		return s
	}
	return string(rs[:max]) + "\n…[truncated]"
}

func acpToolDescriptor(name string, args json.RawMessage) string {
	var obj map[string]any
	if len(args) > 0 {
		_ = json.Unmarshal(args, &obj)
	}
	switch name {
	case "read_file", "read_lines", "path_stat", "write_file", "insert_lines", "str_replace", "patch_file", "remove_path":
		return acpQuoted(obj, "path")
	case "move_path", "copy_path":
		from := acpQuoted(obj, "from")
		to := acpQuoted(obj, "to")
		if from != "" && to != "" {
			return from + " -> " + to
		}
		return strings.TrimSpace(from + " " + to)
	case "grep":
		pat := acpQuoted(obj, "pattern")
		scope := acpQuoted(obj, "path_prefix")
		if scope == "" {
			return pat
		}
		return pat + " in " + scope
	case "list_dir":
		path := acpQuoted(obj, "path")
		if path == "" {
			return "\".\""
		}
		return path
	case "glob":
		pat := acpQuoted(obj, "pattern")
		scope := acpQuoted(obj, "under")
		if scope == "" {
			return pat
		}
		return pat + " under " + scope
	case "search_files":
		sub := acpQuoted(obj, "substring")
		sfx := acpQuoted(obj, "suffix")
		if sub != "" && sfx != "" {
			return sub + " + " + sfx
		}
		if sub != "" {
			return sub
		}
		return sfx
	case "run_command":
		return acpQuoted(obj, "command")
	case "run_shell":
		return acpQuoted(obj, "command")
	case "fetch_url":
		return acpQuoted(obj, "url")
	case "web_search":
		return acpQuoted(obj, "query")
	case "find_references":
		return acpQuoted(obj, "symbol")
	case "unity_query_scene_hierarchy":
		return acpQuoted(obj, "scenePath")
	case "unity_query_prefab_hierarchy":
		return acpQuoted(obj, "prefabAssetPath")
	case "unity_search_asset_database":
		return acpQuoted(obj, "searchFilter")
	case "unity_inspect_component":
		return acpQuoted(obj, "componentTypeName")
	case "unity_apply_actions":
		if obj == nil {
			return ""
		}
		if v, ok := obj["actions"].([]any); ok {
			return fmt.Sprintf("%d actions", len(v))
		}
		return "apply_actions"
	default:
		sum := agentlog.SummarizeArgs(name, args)
		if b, err := json.Marshal(sum); err == nil {
			out := strings.TrimSpace(string(b))
			if out != "" && out != "{}" {
				return out
			}
		}
		return ""
	}
}

func acpQuoted(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return fmt.Sprintf("%q", s)
}
