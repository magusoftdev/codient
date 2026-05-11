package lspclient

import "encoding/json"

// LSP message types — only the subset codient needs.

// --- Initialize ---

type InitializeParams struct {
	ProcessID    int                `json:"processId"`
	RootURI      string             `json:"rootUri"`
	Capabilities ClientCapabilities `json:"capabilities"`
}

type ClientCapabilities struct {
	TextDocument *TextDocumentClientCapabilities `json:"textDocument,omitempty"`
}

type TextDocumentClientCapabilities struct {
	Rename *RenameClientCapabilities `json:"rename,omitempty"`
}

type RenameClientCapabilities struct {
	PrepareSupport bool `json:"prepareSupport,omitempty"`
}

type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
}

type ServerCapabilities struct {
	DefinitionProvider     boolOrObj `json:"definitionProvider,omitempty"`
	TypeDefinitionProvider boolOrObj `json:"typeDefinitionProvider,omitempty"`
	ImplementationProvider boolOrObj `json:"implementationProvider,omitempty"`
	ReferencesProvider     boolOrObj `json:"referencesProvider,omitempty"`
	HoverProvider          boolOrObj `json:"hoverProvider,omitempty"`
	DocumentSymbolProvider boolOrObj `json:"documentSymbolProvider,omitempty"`
	WorkspaceSymbolProvider boolOrObj `json:"workspaceSymbolProvider,omitempty"`
	RenameProvider         boolOrObj `json:"renameProvider,omitempty"`
}

// boolOrObj handles LSP providers that can be true, false, or an options object.
type boolOrObj bool

func (b *boolOrObj) UnmarshalJSON(data []byte) error {
	var v bool
	if err := json.Unmarshal(data, &v); err == nil {
		*b = boolOrObj(v)
		return nil
	}
	// Any non-null object means the capability is supported.
	if string(data) != "null" {
		*b = true
	}
	return nil
}

// --- Text Document ---

type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// --- Definition / TypeDefinition / Implementation ---

type DefinitionParams = TextDocumentPositionParams
type TypeDefinitionParams = TextDocumentPositionParams
type ImplementationParams = TextDocumentPositionParams

type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// --- References ---

type ReferenceParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	Context      ReferenceContext        `json:"context"`
}

type ReferenceContext struct {
	IncludeDeclaration bool `json:"includeDeclaration"`
}

// --- Hover ---

type HoverParams = TextDocumentPositionParams

type HoverResult struct {
	Contents HoverContents `json:"contents"`
	Range    *Range        `json:"range,omitempty"`
}

// HoverContents handles the various shapes of hover content.
type HoverContents struct {
	Value string
}

func (h *HoverContents) UnmarshalJSON(data []byte) error {
	// Try MarkupContent {kind, value} first.
	var mc struct {
		Kind  string `json:"kind"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(data, &mc); err == nil && mc.Value != "" {
		h.Value = mc.Value
		return nil
	}
	// Try plain string.
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		h.Value = s
		return nil
	}
	// Try array of strings or MarkedString objects.
	var arr []json.RawMessage
	if err := json.Unmarshal(data, &arr); err == nil {
		var parts []string
		for _, raw := range arr {
			var str string
			if json.Unmarshal(raw, &str) == nil {
				parts = append(parts, str)
				continue
			}
			var ms struct {
				Language string `json:"language"`
				Value    string `json:"value"`
			}
			if json.Unmarshal(raw, &ms) == nil {
				parts = append(parts, ms.Value)
			}
		}
		h.Value = joinNonEmpty(parts, "\n")
		return nil
	}
	// Fallback: raw string.
	h.Value = string(data)
	return nil
}

func joinNonEmpty(parts []string, sep string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return ""
	}
	result := out[0]
	for _, p := range out[1:] {
		result += sep + p
	}
	return result
}

// --- Document Symbols ---

type DocumentSymbolParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// SymbolInformation is the flat representation returned by older servers.
type SymbolInformation struct {
	Name          string   `json:"name"`
	Kind          int      `json:"kind"`
	Location      Location `json:"location"`
	ContainerName string   `json:"containerName,omitempty"`
}

// DocumentSymbol is the hierarchical representation.
type DocumentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           int              `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

// --- Workspace Symbols ---

type WorkspaceSymbolParams struct {
	Query string `json:"query"`
}

// --- Rename ---

type RenameParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	NewName      string                 `json:"newName"`
}

type WorkspaceEdit struct {
	Changes         map[string][]TextEdit  `json:"changes,omitempty"`
	DocumentChanges []TextDocumentEdit     `json:"documentChanges,omitempty"`
}

type TextDocumentEdit struct {
	TextDocument VersionedTextDocumentIdentifier `json:"textDocument"`
	Edits        []TextEdit                      `json:"edits"`
}

type VersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version *int   `json:"version,omitempty"`
}

// NormalizeChanges merges DocumentChanges into Changes so callers only need
// to inspect one field.
func (we *WorkspaceEdit) NormalizeChanges() {
	if len(we.DocumentChanges) == 0 {
		return
	}
	if we.Changes == nil {
		we.Changes = make(map[string][]TextEdit)
	}
	for _, dc := range we.DocumentChanges {
		uri := dc.TextDocument.URI
		we.Changes[uri] = append(we.Changes[uri], dc.Edits...)
	}
	we.DocumentChanges = nil
}

type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

// Symbol kind constants (subset).
const (
	SKFile          = 1
	SKModule        = 2
	SKNamespace     = 3
	SKPackage       = 4
	SKClass         = 5
	SKMethod        = 6
	SKProperty      = 7
	SKField         = 8
	SKConstructor   = 9
	SKEnum          = 10
	SKInterface     = 11
	SKFunction      = 12
	SKVariable      = 13
	SKConstant      = 14
	SKString        = 15
	SKNumber        = 16
	SKBoolean       = 17
	SKArray         = 18
	SKObject        = 19
	SKKey           = 20
	SKNull          = 21
	SKEnumMember    = 22
	SKStruct        = 23
	SKEvent         = 24
	SKOperator      = 25
	SKTypeParameter = 26
)

var symbolKindNames = map[int]string{
	SKFile: "File", SKModule: "Module", SKNamespace: "Namespace",
	SKPackage: "Package", SKClass: "Class", SKMethod: "Method",
	SKProperty: "Property", SKField: "Field", SKConstructor: "Constructor",
	SKEnum: "Enum", SKInterface: "Interface", SKFunction: "Function",
	SKVariable: "Variable", SKConstant: "Constant", SKString: "String",
	SKNumber: "Number", SKBoolean: "Boolean", SKArray: "Array",
	SKObject: "Object", SKKey: "Key", SKNull: "Null",
	SKEnumMember: "EnumMember", SKStruct: "Struct", SKEvent: "Event",
	SKOperator: "Operator", SKTypeParameter: "TypeParameter",
}

// SymbolKindName returns the display name for a symbol kind value.
func SymbolKindName(kind int) string {
	if n, ok := symbolKindNames[kind]; ok {
		return n
	}
	return "Unknown"
}
