package lspclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"codient/internal/config"
	"codient/internal/sandbox"
)

type serverConn struct {
	id   string
	cmd  *exec.Cmd
	conn *conn
	caps ServerCapabilities
	root string

	mu     sync.Mutex
	opened map[string]bool // URI -> true for files we've sent didOpen
}

// Manager holds LSP client state: one long-lived child process per language server.
type Manager struct {
	mu      sync.RWMutex
	servers map[string]*serverConn
	root    string
}

// NewManager creates a Manager. Call Connect to start servers.
func NewManager() *Manager {
	return &Manager{
		servers: make(map[string]*serverConn),
	}
}

// Connect starts all configured language servers eagerly. Failures are
// collected as warnings rather than aborting the session.
func (m *Manager) Connect(ctx context.Context, servers map[string]config.LSPServerConfig, workspaceRoot string) []string {
	m.root = workspaceRoot
	var warnings []string
	for id, cfg := range servers {
		sc, err := m.startServer(ctx, id, cfg, workspaceRoot)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("lsp %s: %v", id, err))
			continue
		}
		m.mu.Lock()
		m.servers[id] = sc
		m.mu.Unlock()
	}
	return warnings
}

func (m *Manager) startServer(ctx context.Context, id string, cfg config.LSPServerConfig, root string) (*serverConn, error) {
	if cfg.Command == "" {
		return nil, fmt.Errorf("lsp server config must set command")
	}
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	cmd.Dir = root
	cmd.Env = mergeProcessEnv(cfg.Env)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = nil // discard server stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", cfg.Command, err)
	}

	c := newConn(stdout, stdin)

	rootURI := fileURI(root)
	initParams := InitializeParams{
		ProcessID: os.Getpid(),
		RootURI:   rootURI,
		Capabilities: ClientCapabilities{
			TextDocument: &TextDocumentClientCapabilities{
				Rename: &RenameClientCapabilities{PrepareSupport: false},
			},
		},
	}

	var result InitializeResult
	if err := c.call(ctx, "initialize", initParams, &result); err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("initialize: %w", err)
	}

	if err := c.notify("initialized", struct{}{}); err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("initialized notification: %w", err)
	}

	return &serverConn{
		id:     id,
		cmd:    cmd,
		conn:   c,
		caps:   result.Capabilities,
		root:   root,
		opened: make(map[string]bool),
	}, nil
}

// Close shuts down all language servers gracefully.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, sc := range m.servers {
		shutdownCtx := context.Background()
		_ = sc.conn.call(shutdownCtx, "shutdown", nil, nil)
		_ = sc.conn.notify("exit", nil)
		_ = sc.conn.close()
		if sc.cmd.Process != nil {
			_ = sc.cmd.Process.Kill()
		}
	}
	m.servers = make(map[string]*serverConn)
}

// ServerIDs returns the IDs of all connected servers.
func (m *Manager) ServerIDs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := make([]string, 0, len(m.servers))
	for id := range m.servers {
		ids = append(ids, id)
	}
	return ids
}

// ServerCapabilities returns the capabilities for a specific server.
func (m *Manager) ServerCapabilities(id string) *ServerCapabilities {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sc, ok := m.servers[id]
	if !ok {
		return nil
	}
	return &sc.caps
}

// PickServer selects a server by explicit language override, or by matching
// the file extension against configured file_extensions. Returns nil if no
// server matches. The returned handle is opaque to callers outside this package.
func (m *Manager) PickServer(file string, languageOverride string, lspCfg map[string]config.LSPServerConfig) interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if languageOverride != "" {
		if sc, ok := m.servers[languageOverride]; ok {
			return sc
		}
	}

	ext := strings.ToLower(filepath.Ext(file))
	for id, cfg := range lspCfg {
		sc, ok := m.servers[id]
		if !ok {
			continue
		}
		for _, fe := range cfg.FileExtensions {
			if strings.ToLower(fe) == ext || "."+strings.ToLower(strings.TrimPrefix(fe, ".")) == ext {
				return sc
			}
		}
	}
	return nil
}

func asServerConn(h interface{}) *serverConn {
	sc, _ := h.(*serverConn)
	return sc
}

// ensureOpen sends textDocument/didOpen if we haven't already for this URI on this server.
func (sc *serverConn) ensureOpen(absPath string) error {
	uri := fileURI(absPath)
	sc.mu.Lock()
	if sc.opened[uri] {
		sc.mu.Unlock()
		return nil
	}
	sc.opened[uri] = true
	sc.mu.Unlock()

	content, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("read file for didOpen: %w", err)
	}

	langID := guessLanguageID(absPath)
	return sc.conn.notify("textDocument/didOpen", DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{
			URI:        uri,
			LanguageID: langID,
			Version:    1,
			Text:       string(content),
		},
	})
}

// Definition calls textDocument/definition.
func (m *Manager) Definition(ctx context.Context, h interface{}, absPath string, line, char int) ([]Location, error) {
	sc := asServerConn(h)
	if sc == nil {
		return nil, fmt.Errorf("invalid server handle")
	}
	if !bool(sc.caps.DefinitionProvider) {
		return nil, fmt.Errorf("server %s does not support definition", sc.id)
	}
	if err := sc.ensureOpen(absPath); err != nil {
		return nil, err
	}
	params := DefinitionParams{
		TextDocument: TextDocumentIdentifier{URI: fileURI(absPath)},
		Position:     Position{Line: line, Character: char},
	}
	return m.locationRequest(ctx, sc, "textDocument/definition", params)
}

// TypeDefinition calls textDocument/typeDefinition.
func (m *Manager) TypeDefinition(ctx context.Context, h interface{}, absPath string, line, char int) ([]Location, error) {
	sc := asServerConn(h)
	if sc == nil {
		return nil, fmt.Errorf("invalid server handle")
	}
	if !bool(sc.caps.TypeDefinitionProvider) {
		return nil, fmt.Errorf("server %s does not support typeDefinition", sc.id)
	}
	if err := sc.ensureOpen(absPath); err != nil {
		return nil, err
	}
	params := TypeDefinitionParams{
		TextDocument: TextDocumentIdentifier{URI: fileURI(absPath)},
		Position:     Position{Line: line, Character: char},
	}
	return m.locationRequest(ctx, sc, "textDocument/typeDefinition", params)
}

// Implementation calls textDocument/implementation.
func (m *Manager) Implementation(ctx context.Context, h interface{}, absPath string, line, char int) ([]Location, error) {
	sc := asServerConn(h)
	if sc == nil {
		return nil, fmt.Errorf("invalid server handle")
	}
	if !bool(sc.caps.ImplementationProvider) {
		return nil, fmt.Errorf("server %s does not support implementation", sc.id)
	}
	if err := sc.ensureOpen(absPath); err != nil {
		return nil, err
	}
	params := ImplementationParams{
		TextDocument: TextDocumentIdentifier{URI: fileURI(absPath)},
		Position:     Position{Line: line, Character: char},
	}
	return m.locationRequest(ctx, sc, "textDocument/implementation", params)
}

// References calls textDocument/references.
func (m *Manager) References(ctx context.Context, h interface{}, absPath string, line, char int) ([]Location, error) {
	sc := asServerConn(h)
	if sc == nil {
		return nil, fmt.Errorf("invalid server handle")
	}
	if !bool(sc.caps.ReferencesProvider) {
		return nil, fmt.Errorf("server %s does not support references", sc.id)
	}
	if err := sc.ensureOpen(absPath); err != nil {
		return nil, err
	}
	params := ReferenceParams{
		TextDocument: TextDocumentIdentifier{URI: fileURI(absPath)},
		Position:     Position{Line: line, Character: char},
		Context:      ReferenceContext{IncludeDeclaration: true},
	}
	return m.locationRequest(ctx, sc, "textDocument/references", params)
}

// locationRequest handles definition/references/typeDefinition/implementation which
// all return Location | Location[] | null.
func (m *Manager) locationRequest(ctx context.Context, sc *serverConn, method string, params any) ([]Location, error) {
	var raw json.RawMessage
	if err := sc.conn.call(ctx, method, params, &raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	// Try []Location first.
	var locs []Location
	if err := json.Unmarshal(raw, &locs); err == nil {
		return locs, nil
	}
	// Try single Location.
	var loc Location
	if err := json.Unmarshal(raw, &loc); err == nil {
		return []Location{loc}, nil
	}
	return nil, fmt.Errorf("unexpected %s response: %s", method, string(raw))
}

// Hover calls textDocument/hover.
func (m *Manager) Hover(ctx context.Context, h interface{}, absPath string, line, char int) (*HoverResult, error) {
	sc := asServerConn(h)
	if sc == nil {
		return nil, fmt.Errorf("invalid server handle")
	}
	if !bool(sc.caps.HoverProvider) {
		return nil, fmt.Errorf("server %s does not support hover", sc.id)
	}
	if err := sc.ensureOpen(absPath); err != nil {
		return nil, err
	}
	params := HoverParams{
		TextDocument: TextDocumentIdentifier{URI: fileURI(absPath)},
		Position:     Position{Line: line, Character: char},
	}
	var raw json.RawMessage
	if err := sc.conn.call(ctx, "textDocument/hover", params, &raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var result HoverResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("parse hover result: %w", err)
	}
	return &result, nil
}

// DocumentSymbols calls textDocument/documentSymbol.
func (m *Manager) DocumentSymbols(ctx context.Context, h interface{}, absPath string) ([]SymbolInformation, error) {
	sc := asServerConn(h)
	if sc == nil {
		return nil, fmt.Errorf("invalid server handle")
	}
	if !bool(sc.caps.DocumentSymbolProvider) {
		return nil, fmt.Errorf("server %s does not support documentSymbol", sc.id)
	}
	if err := sc.ensureOpen(absPath); err != nil {
		return nil, err
	}
	params := DocumentSymbolParams{
		TextDocument: TextDocumentIdentifier{URI: fileURI(absPath)},
	}
	var raw json.RawMessage
	if err := sc.conn.call(ctx, "textDocument/documentSymbol", params, &raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	// Try []SymbolInformation (flat).
	var flat []SymbolInformation
	if err := json.Unmarshal(raw, &flat); err == nil && len(flat) > 0 && flat[0].Name != "" {
		return flat, nil
	}
	// Try []DocumentSymbol (hierarchical) and flatten.
	var hier []DocumentSymbol
	if err := json.Unmarshal(raw, &hier); err == nil {
		uri := fileURI(absPath)
		return flattenDocSymbols(hier, uri, ""), nil
	}
	return nil, nil
}

func flattenDocSymbols(syms []DocumentSymbol, uri, container string) []SymbolInformation {
	var out []SymbolInformation
	for _, s := range syms {
		out = append(out, SymbolInformation{
			Name:          s.Name,
			Kind:          s.Kind,
			Location:      Location{URI: uri, Range: s.Range},
			ContainerName: container,
		})
		out = append(out, flattenDocSymbols(s.Children, uri, s.Name)...)
	}
	return out
}

// WorkspaceSymbols calls workspace/symbol.
func (m *Manager) WorkspaceSymbols(ctx context.Context, h interface{}, query string) ([]SymbolInformation, error) {
	sc := asServerConn(h)
	if sc == nil {
		return nil, fmt.Errorf("invalid server handle")
	}
	if !bool(sc.caps.WorkspaceSymbolProvider) {
		return nil, fmt.Errorf("server %s does not support workspace/symbol", sc.id)
	}
	params := WorkspaceSymbolParams{Query: query}
	var result []SymbolInformation
	if err := sc.conn.call(ctx, "workspace/symbol", params, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// Rename calls textDocument/rename and returns the WorkspaceEdit.
func (m *Manager) Rename(ctx context.Context, h interface{}, absPath string, line, char int, newName string) (*WorkspaceEdit, error) {
	sc := asServerConn(h)
	if sc == nil {
		return nil, fmt.Errorf("invalid server handle")
	}
	if !bool(sc.caps.RenameProvider) {
		return nil, fmt.Errorf("server %s does not support rename", sc.id)
	}
	if err := sc.ensureOpen(absPath); err != nil {
		return nil, err
	}
	params := RenameParams{
		TextDocument: TextDocumentIdentifier{URI: fileURI(absPath)},
		Position:     Position{Line: line, Character: char},
		NewName:      newName,
	}
	var result WorkspaceEdit
	if err := sc.conn.call(ctx, "textDocument/rename", params, &result); err != nil {
		return nil, err
	}
	result.NormalizeChanges()
	return &result, nil
}

// --- helpers ---

func mergeProcessEnv(extra map[string]string) []string {
	m := make(map[string]string)
	for _, e := range sandbox.ScrubOSEnviron(nil) {
		k, v, ok := strings.Cut(e, "=")
		if !ok {
			continue
		}
		m[k] = v
	}
	for k, v := range extra {
		m[k] = v
	}
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

func fileURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	u := &url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	return u.String()
}

// ParseFileURI converts a file:// URI back to an absolute filesystem path.
func ParseFileURI(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("parse URI: %w", err)
	}
	if u.Scheme != "file" {
		return "", fmt.Errorf("not a file URI: %s", uri)
	}
	return filepath.FromSlash(u.Path), nil
}

func guessLanguageID(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js":
		return "javascript"
	case ".jsx":
		return "javascriptreact"
	case ".ts":
		return "typescript"
	case ".tsx":
		return "typescriptreact"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".c":
		return "c"
	case ".cpp", ".cc", ".cxx":
		return "cpp"
	case ".h", ".hpp":
		return "cpp"
	case ".cs":
		return "csharp"
	case ".rb":
		return "ruby"
	case ".lua":
		return "lua"
	case ".sh", ".bash":
		return "shellscript"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".xml":
		return "xml"
	case ".html", ".htm":
		return "html"
	case ".css":
		return "css"
	case ".md":
		return "markdown"
	default:
		return "plaintext"
	}
}
