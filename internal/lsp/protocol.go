package lsp

import (
	"encoding/json"

	"github.com/jhaton/janet-lsp/internal/janet"
)

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type initializeParams struct {
	RootURI          string            `json:"rootUri"`
	WorkspaceFolders []workspaceFolder `json:"workspaceFolders"`
}

type workspaceFolder struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}

type textDocumentIdentifier struct {
	URI string `json:"uri"`
}

type versionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

type textDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

type didOpenParams struct {
	TextDocument textDocumentItem `json:"textDocument"`
}

type didChangeParams struct {
	TextDocument   versionedTextDocumentIdentifier `json:"textDocument"`
	ContentChanges []struct {
		Text string `json:"text"`
	} `json:"contentChanges"`
}

type didCloseParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
}

type documentPositionParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Position     janet.Position         `json:"position"`
}
type documentFormattingParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
}

type referenceParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Position     janet.Position         `json:"position"`
	Context      struct {
		IncludeDeclaration bool `json:"includeDeclaration"`
	} `json:"context"`
}

type renameParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Position     janet.Position         `json:"position"`
	NewName      string                 `json:"newName"`
}

type location struct {
	URI   string      `json:"uri"`
	Range janet.Range `json:"range"`
}

type textEdit struct {
	Range   janet.Range `json:"range"`
	NewText string      `json:"newText"`
}

type workspaceEdit struct {
	Changes map[string][]textEdit `json:"changes"`
}

type diagnostic struct {
	Range    janet.Range `json:"range"`
	Severity int         `json:"severity"`
	Source   string      `json:"source"`
	Message  string      `json:"message"`
}

type completionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []completionItem `json:"items"`
}

type completionItem struct {
	Label string `json:"label"`
	Kind  int    `json:"kind"`
}

type hover struct {
	Contents markupContent `json:"contents"`
	Range    janet.Range   `json:"range"`
}

type markupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type signatureHelp struct {
	Signatures []signatureInformation `json:"signatures"`
}

type signatureInformation struct {
	Label         string         `json:"label"`
	Documentation *markupContent `json:"documentation,omitempty"`
}

type documentDiagnosticReport struct {
	Kind  string       `json:"kind"`
	Items []diagnostic `json:"items"`
}

type documentSymbol struct {
	Name           string      `json:"name"`
	Detail         string      `json:"detail,omitempty"`
	Kind           int         `json:"kind"`
	Range          janet.Range `json:"range"`
	SelectionRange janet.Range `json:"selectionRange"`
}
