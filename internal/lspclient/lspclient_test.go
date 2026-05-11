package lspclient

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
)

// stubLSPServer runs a minimal LSP server over a net.Conn using proper
// Content-Length framing. It handles initialize, textDocument/definition,
// textDocument/hover, workspace/symbol, textDocument/rename, and shutdown.
func stubLSPServer(t *testing.T, rw io.ReadWriteCloser) {
	t.Helper()
	reader := bufio.NewReader(rw)
	for {
		body, err := readFramedMessage(reader)
		if err != nil {
			return
		}
		var req struct {
			ID     *int64          `json:"id,omitempty"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			continue
		}
		if req.ID == nil {
			continue // notification
		}
		var result any
		switch req.Method {
		case "initialize":
			result = InitializeResult{
				Capabilities: ServerCapabilities{
					DefinitionProvider:      true,
					ReferencesProvider:      true,
					HoverProvider:           true,
					DocumentSymbolProvider:  true,
					WorkspaceSymbolProvider: true,
					RenameProvider:          true,
				},
			}
		case "textDocument/definition":
			result = []Location{{
				URI:   "file:///tmp/test/main.go",
				Range: Range{Start: Position{Line: 9, Character: 5}, End: Position{Line: 9, Character: 10}},
			}}
		case "textDocument/hover":
			result = HoverResult{
				Contents: HoverContents{Value: "func Hello()"},
			}
		case "workspace/symbol":
			result = []SymbolInformation{{
				Name:     "Hello",
				Kind:     SKFunction,
				Location: Location{URI: "file:///tmp/test/main.go", Range: Range{Start: Position{Line: 5, Character: 0}}},
			}}
		case "textDocument/rename":
			result = WorkspaceEdit{
				Changes: map[string][]TextEdit{
					"file:///tmp/test/main.go": {{
						Range:   Range{Start: Position{Line: 5, Character: 5}, End: Position{Line: 5, Character: 10}},
						NewText: "Goodbye",
					}},
				},
			}
		case "shutdown":
			result = nil
		default:
			continue
		}
		resp := jsonrpcResponse{JSONRPC: "2.0", ID: *req.ID}
		if result != nil {
			b, _ := json.Marshal(result)
			resp.Result = b
		} else {
			resp.Result = json.RawMessage("null")
		}
		writeFramedMessage(rw, resp)
	}
}

func readFramedMessage(r *bufio.Reader) ([]byte, error) {
	var contentLen int
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "Content-Length:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
			n, err := strconv.Atoi(val)
			if err != nil {
				return nil, fmt.Errorf("bad Content-Length: %w", err)
			}
			contentLen = n
		}
	}
	if contentLen == 0 {
		return nil, fmt.Errorf("missing Content-Length")
	}
	body := make([]byte, contentLen)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}

func writeFramedMessage(w io.Writer, msg any) {
	body, _ := json.Marshal(msg)
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	_, _ = io.WriteString(w, header)
	_, _ = w.Write(body)
}

type pipeCloser struct {
	io.Reader
	io.Writer
	conn net.Conn
}

func (p pipeCloser) Close() error { return p.conn.Close() }

func TestConn_CallAndResponse(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	go stubLSPServer(t, serverConn)

	clientR := pipeCloser{Reader: clientConn, conn: clientConn}
	clientW := pipeCloser{Writer: clientConn, conn: clientConn}
	c := newConn(clientR, clientW)
	defer c.close()

	ctx := context.Background()

	// Test initialize.
	var initResult InitializeResult
	if err := c.call(ctx, "initialize", InitializeParams{
		ProcessID: 1,
		RootURI:   "file:///tmp/test",
	}, &initResult); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if !bool(initResult.Capabilities.DefinitionProvider) {
		t.Fatal("expected definition provider")
	}

	// Test definition.
	var locs []Location
	if err := c.call(ctx, "textDocument/definition", DefinitionParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///tmp/test/main.go"},
		Position:     Position{Line: 5, Character: 5},
	}, &locs); err != nil {
		t.Fatalf("definition: %v", err)
	}
	if len(locs) != 1 {
		t.Fatalf("expected 1 location, got %d", len(locs))
	}
	if locs[0].Range.Start.Line != 9 {
		t.Fatalf("expected line 9, got %d", locs[0].Range.Start.Line)
	}

	// Test hover.
	var hover HoverResult
	if err := c.call(ctx, "textDocument/hover", HoverParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///tmp/test/main.go"},
		Position:     Position{Line: 5, Character: 5},
	}, &hover); err != nil {
		t.Fatalf("hover: %v", err)
	}
	if hover.Contents.Value != "func Hello()" {
		t.Fatalf("hover contents: %q", hover.Contents.Value)
	}

	// Test workspace/symbol.
	var syms []SymbolInformation
	if err := c.call(ctx, "workspace/symbol", WorkspaceSymbolParams{Query: "Hello"}, &syms); err != nil {
		t.Fatalf("workspace/symbol: %v", err)
	}
	if len(syms) != 1 || syms[0].Name != "Hello" {
		t.Fatalf("workspace/symbol: %+v", syms)
	}
}

func TestBoolOrObj_Unmarshal(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{`true`, true},
		{`false`, false},
		{`null`, false},
		{`{"triggerCharacters":["."]}`, true},
		{`{}`, true},
	}
	for _, tc := range cases {
		var b boolOrObj
		if err := json.Unmarshal([]byte(tc.input), &b); err != nil {
			t.Errorf("unmarshal %s: %v", tc.input, err)
			continue
		}
		if bool(b) != tc.want {
			t.Errorf("input=%s: got %v, want %v", tc.input, bool(b), tc.want)
		}
	}
}

func TestHoverContents_Unmarshal(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{`{"kind":"markdown","value":"# hello"}`, "# hello"},
		{`"plain text"`, "plain text"},
		{`[{"language":"go","value":"func X()"}]`, "func X()"},
	}
	for _, tc := range cases {
		var h HoverContents
		if err := json.Unmarshal([]byte(tc.input), &h); err != nil {
			t.Errorf("unmarshal %s: %v", tc.input, err)
			continue
		}
		if h.Value != tc.want {
			t.Errorf("input=%s: got %q, want %q", tc.input, h.Value, tc.want)
		}
	}
}

func TestSymbolKindName(t *testing.T) {
	if got := SymbolKindName(SKFunction); got != "Function" {
		t.Errorf("got %q", got)
	}
	if got := SymbolKindName(999); got != "Unknown" {
		t.Errorf("got %q", got)
	}
}

func TestGuessLanguageID(t *testing.T) {
	cases := map[string]string{
		"main.go":      "go",
		"app.py":       "python",
		"index.tsx":    "typescriptreact",
		"readme.md":    "markdown",
		"file.unknown": "plaintext",
	}
	for path, want := range cases {
		got := guessLanguageID(path)
		if got != want {
			t.Errorf("guessLanguageID(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestFileURI_RoundTrip(t *testing.T) {
	path := "/tmp/test/main.go"
	uri := fileURI(path)
	back, err := ParseFileURI(uri)
	if err != nil {
		t.Fatalf("ParseFileURI: %v", err)
	}
	if back != path {
		t.Fatalf("round-trip: %q -> %q -> %q", path, uri, back)
	}
}

func TestParseFileURI_Error(t *testing.T) {
	_, err := ParseFileURI("https://example.com")
	if err == nil {
		t.Fatal("expected error for non-file URI")
	}
}
