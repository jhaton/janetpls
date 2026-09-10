package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jhaton/janetpls/internal/janet"
)

const Version = "0.5.1"

type openDocument struct {
	Text    string
	Version int
}

type Server struct {
	connection *connection
	stderr     io.Writer

	rootMu sync.RWMutex
	root   string

	documentsMu sync.RWMutex
	documents   map[string]openDocument

	cancellationsMu sync.Mutex
	cancellations   map[string]context.CancelFunc
	requests        sync.WaitGroup
	shutdown        atomic.Bool

	compilerEnabled     bool
	compilerCommand     []string
	compilerTimeout     time.Duration
	compilerFailureOnce sync.Once
}

func NewServer(reader io.Reader, writer io.Writer, stderr io.Writer) (*Server, error) {
	root, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return &Server{
		connection:    newConnection(reader, writer),
		stderr:        stderr,
		root:          root,
		documents:     make(map[string]openDocument),
		cancellations: make(map[string]context.CancelFunc),
	}, nil
}

func (server *Server) Run(ctx context.Context) error {
	for {
		message, err := server.connection.read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				server.requests.Wait()
				return nil
			}
			return err
		}
		if message.Method == "exit" {
			server.cancelAll()
			server.requests.Wait()
			if server.shutdown.Load() {
				return nil
			}
			return errors.New("exit received before shutdown")
		}
		if len(message.ID) == 0 || string(message.ID) == "null" {
			server.handleNotification(ctx, message)
			continue
		}
		if message.Method == "initialize" || message.Method == "shutdown" {
			server.handleRequestAndRespond(ctx, message)
			continue
		}

		requestContext, cancel := context.WithCancel(ctx)
		key := string(message.ID)
		server.cancellationsMu.Lock()
		server.cancellations[key] = cancel
		server.cancellationsMu.Unlock()
		server.requests.Add(1)
		go func(requestContext context.Context, message rpcMessage, key string, cancel context.CancelFunc) {
			defer server.requests.Done()
			defer cancel()
			defer func() {
				server.cancellationsMu.Lock()
				delete(server.cancellations, key)
				server.cancellationsMu.Unlock()
			}()
			server.handleRequestAndRespond(requestContext, message)
		}(requestContext, message, key, cancel)
	}
}

func (server *Server) handleRequestAndRespond(ctx context.Context, message rpcMessage) {
	defer func() {
		if recovered := recover(); recovered != nil {
			fmt.Fprintf(server.stderr, "panic handling %s: %v\n%s", message.Method, recovered, debug.Stack())
			_ = server.respond(message.ID, nil, &rpcError{Code: -32603, Message: "internal server error"})
		}
	}()
	result, responseError := server.handleRequest(ctx, message.Method, message.Params)
	if ctx.Err() != nil {
		responseError = &rpcError{Code: -32800, Message: "request cancelled"}
		result = nil
	}
	if err := server.respond(message.ID, result, responseError); err != nil {
		fmt.Fprintf(server.stderr, "write response for %s: %v\n", message.Method, err)
	}
}

func (server *Server) respond(id json.RawMessage, result any, responseError *rpcError) error {
	return server.connection.write(rpcMessage{ID: id, Result: result, Error: responseError})
}

func (server *Server) notify(method string, params any) error {
	encoded, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return server.connection.write(rpcMessage{Method: method, Params: encoded})
}

func (server *Server) handleNotification(ctx context.Context, message rpcMessage) {
	switch message.Method {
	case "initialized", "$/setTrace":
		return
	case "$/cancelRequest":
		var params struct {
			ID json.RawMessage `json:"id"`
		}
		if json.Unmarshal(message.Params, &params) == nil {
			server.cancellationsMu.Lock()
			cancel := server.cancellations[string(params.ID)]
			server.cancellationsMu.Unlock()
			if cancel != nil {
				cancel()
			}
		}
	case "textDocument/didOpen":
		var params didOpenParams
		if json.Unmarshal(message.Params, &params) != nil {
			return
		}
		uri := canonicalURI(params.TextDocument.URI)
		server.documentsMu.Lock()
		server.documents[uri] = openDocument{Text: params.TextDocument.Text, Version: params.TextDocument.Version}
		server.documentsMu.Unlock()
	case "textDocument/didChange":
		var params didChangeParams
		if json.Unmarshal(message.Params, &params) != nil || len(params.ContentChanges) == 0 {
			return
		}
		uri := canonicalURI(params.TextDocument.URI)
		text := params.ContentChanges[len(params.ContentChanges)-1].Text
		server.documentsMu.Lock()
		server.documents[uri] = openDocument{Text: text, Version: params.TextDocument.Version}
		server.documentsMu.Unlock()
	case "textDocument/didClose":
		var params didCloseParams
		if json.Unmarshal(message.Params, &params) != nil {
			return
		}
		uri := canonicalURI(params.TextDocument.URI)
		server.documentsMu.Lock()
		delete(server.documents, uri)
		server.documentsMu.Unlock()
	}
}

func (server *Server) handleRequest(ctx context.Context, method string, rawParams json.RawMessage) (any, *rpcError) {
	switch method {
	case "initialize":
		var params initializeParams
		if err := json.Unmarshal(rawParams, &params); err != nil {
			return nil, invalidParams(err)
		}
		rootURI := params.RootURI
		if len(params.WorkspaceFolders) > 0 {
			rootURI = params.WorkspaceFolders[0].URI
		}
		if rootURI != "" {
			root, err := janet.URIToPath(rootURI)
			if err != nil {
				return nil, invalidParams(err)
			}
			server.rootMu.Lock()
			server.root = root
			server.rootMu.Unlock()
		}
		server.configureCompiler(params.InitializationOptions)
		return initializeResult(), nil
	case "shutdown":
		server.shutdown.Store(true)
		return nil, nil
	case "textDocument/definition":
		return server.definition(ctx, rawParams)
	case "textDocument/references":
		return server.references(ctx, rawParams)
	case "textDocument/prepareRename":
		return server.prepareRename(ctx, rawParams)
	case "textDocument/rename":
		return server.rename(ctx, rawParams)
	case "textDocument/hover":
		return server.hover(ctx, rawParams)
	case "textDocument/completion":
		return server.completion(ctx, rawParams)
	case "textDocument/signatureHelp":
		return server.signatureHelp(ctx, rawParams)
	case "textDocument/documentSymbol":
		return server.documentSymbols(ctx, rawParams)
	case "textDocument/diagnostic":
		return server.documentDiagnostics(ctx, rawParams)
	case "textDocument/formatting":
		return server.formatting(ctx, rawParams)
	default:
		return nil, &rpcError{Code: -32601, Message: "method not found: " + method}
	}
}

func initializeResult() map[string]any {
	return map[string]any{
		"capabilities": map[string]any{
			"positionEncoding":           "utf-16",
			"textDocumentSync":           map[string]any{"openClose": true, "change": 1},
			"definitionProvider":         true,
			"referencesProvider":         true,
			"renameProvider":             map[string]any{"prepareProvider": true},
			"hoverProvider":              true,
			"completionProvider":         map[string]any{"resolveProvider": false, "triggerCharacters": []string{"/"}},
			"signatureHelpProvider":      map[string]any{"triggerCharacters": []string{" "}},
			"documentSymbolProvider":     true,
			"diagnosticProvider":         map[string]any{"interFileDependencies": true, "workspaceDiagnostics": false},
			"documentFormattingProvider": true,
		},
		"serverInfo": map[string]any{"name": "janetpls", "version": Version},
	}
}

func (server *Server) buildIndex(ctx context.Context) (*janet.Index, *rpcError) {
	server.rootMu.RLock()
	root := server.root
	server.rootMu.RUnlock()
	server.documentsMu.RLock()
	overlays := make(map[string]string, len(server.documents))
	for uri, document := range server.documents {
		overlays[uri] = document.Text
	}
	server.documentsMu.RUnlock()
	index, err := janet.BuildIndex(ctx, root, overlays)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, &rpcError{Code: -32800, Message: "request cancelled"}
		}
		return nil, &rpcError{Code: -32603, Message: err.Error()}
	}
	return index, nil
}

func (server *Server) definition(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	_, index, occurrence, responseError := server.positionRequest(ctx, raw)
	if responseError != nil || occurrence == nil {
		return nil, responseError
	}
	definition, ok := index.DefinitionFor(*occurrence)
	if !ok {
		return nil, nil
	}
	document := index.Documents[definition.URI]
	return location{URI: definition.URI, Range: document.Range(definition.Start, definition.End)}, nil
}

func (server *Server) references(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var params referenceParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, invalidParams(err)
	}
	index, responseError := server.buildIndex(ctx)
	if responseError != nil {
		return nil, responseError
	}
	document := index.Document(params.TextDocument.URI)
	if document == nil {
		return []location{}, nil
	}
	occurrence, ok := index.OccurrenceAt(document.URI, document.Offset(params.Position))
	if !ok {
		return []location{}, nil
	}
	references := index.References(occurrence.ID, params.Context.IncludeDeclaration)
	locations := make([]location, 0, len(references))
	for _, reference := range references {
		referenceDocument := index.Documents[reference.URI]
		locations = append(locations, location{URI: reference.URI, Range: referenceDocument.Range(reference.Start, reference.End)})
	}
	return locations, nil
}

func (server *Server) prepareRename(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	_, index, occurrence, responseError := server.positionRequest(ctx, raw)
	if responseError != nil {
		return nil, responseError
	}
	if occurrence == nil {
		return nil, &rpcError{Code: -32602, Message: "symbol cannot be renamed"}
	}
	document := index.Documents[occurrence.URI]
	placeholder := occurrence.Name
	if definition, ok := index.DefinitionFor(*occurrence); ok {
		placeholder = definition.Name
	}
	return map[string]any{"range": document.Range(occurrence.Start, occurrence.End), "placeholder": placeholder}, nil
}

func (server *Server) rename(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var params renameParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, invalidParams(err)
	}
	index, responseError := server.buildIndex(ctx)
	if responseError != nil {
		return nil, responseError
	}
	document := index.Document(params.TextDocument.URI)
	if document == nil {
		return nil, &rpcError{Code: -32602, Message: "document is outside the workspace"}
	}
	occurrence, ok := index.OccurrenceAt(document.URI, document.Offset(params.Position))
	if !ok {
		return nil, &rpcError{Code: -32602, Message: "symbol cannot be renamed"}
	}
	changes, err := index.Rename(occurrence.ID, params.NewName)
	if err != nil {
		return nil, &rpcError{Code: -32602, Message: err.Error()}
	}
	edits := make(map[string][]textEdit, len(changes))
	for uri, replacements := range changes {
		referenceDocument := index.Documents[uri]
		for _, replacement := range replacements {
			edits[uri] = append(edits[uri], textEdit{
				Range:   referenceDocument.Range(replacement.Start, replacement.End),
				NewText: replacement.NewText,
			})
		}
	}
	return workspaceEdit{Changes: edits}, nil
}

func (server *Server) hover(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	_, index, occurrence, responseError := server.positionRequest(ctx, raw)
	if responseError != nil || occurrence == nil {
		return nil, responseError
	}
	definition, ok := index.DefinitionFor(*occurrence)
	if !ok {
		return nil, nil
	}
	value := "```janet\n" + definition.Signature + "\n```"
	if definition.Documentation != "" {
		value += "\n\n" + definition.Documentation
	}
	document := index.Documents[occurrence.URI]
	return hover{
		Contents: markupContent{Kind: "markdown", Value: value},
		Range:    document.Range(occurrence.Start, occurrence.End),
	}, nil
}

func (server *Server) completion(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var params documentPositionParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, invalidParams(err)
	}
	index, responseError := server.buildIndex(ctx)
	if responseError != nil {
		return nil, responseError
	}
	document := index.Document(params.TextDocument.URI)
	if document == nil {
		return completionList{Items: []completionItem{}}, nil
	}
	candidates := index.Completions(document.URI, document.Offset(params.Position))
	items := make([]completionItem, 0, len(candidates))
	for _, candidate := range candidates {
		kind := 6
		if candidate.Kind == "function" {
			kind = 3
		}
		items = append(items, completionItem{Label: candidate.Name, Kind: kind})
	}
	return completionList{IsIncomplete: false, Items: items}, nil
}

func (server *Server) signatureHelp(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var params documentPositionParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, invalidParams(err)
	}
	index, responseError := server.buildIndex(ctx)
	if responseError != nil {
		return nil, responseError
	}
	document := index.Document(params.TextDocument.URI)
	if document == nil {
		return nil, nil
	}
	definition, ok := index.SignatureAt(document.URI, document.Offset(params.Position))
	if !ok {
		return nil, nil
	}
	information := signatureInformation{Label: definition.Signature}
	if definition.Documentation != "" {
		information.Documentation = &markupContent{Kind: "markdown", Value: definition.Documentation}
	}
	return signatureHelp{Signatures: []signatureInformation{information}}, nil
}

func (server *Server) documentSymbols(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var params struct {
		TextDocument textDocumentIdentifier `json:"textDocument"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, invalidParams(err)
	}
	index, responseError := server.buildIndex(ctx)
	if responseError != nil {
		return nil, responseError
	}
	document := index.Document(params.TextDocument.URI)
	if document == nil {
		return []documentSymbol{}, nil
	}
	definitions := index.TopLevelDefinitions(document.URI)
	symbols := make([]documentSymbol, 0, len(definitions))
	for _, definition := range definitions {
		kind := 13
		if definition.Kind == "function" {
			kind = 12
		}
		rangeValue := document.Range(definition.Start, definition.End)
		symbols = append(symbols, documentSymbol{
			Name: definition.Name, Detail: definition.Signature, Kind: kind,
			Range: rangeValue, SelectionRange: rangeValue,
		})
	}
	return symbols, nil
}
func (server *Server) formatting(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var params documentFormattingParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, invalidParams(err)
	}
	if err := ctx.Err(); err != nil {
		return nil, &rpcError{Code: -32800, Message: "request cancelled"}
	}
	uri := canonicalURI(params.TextDocument.URI)
	server.documentsMu.RLock()
	open, isOpen := server.documents[uri]
	server.documentsMu.RUnlock()

	var source string
	var path string
	if isOpen {
		source = open.Text
		path, _ = janet.URIToPath(uri)
	} else {
		var err error
		path, err = janet.URIToPath(uri)
		if err != nil {
			return nil, invalidParams(err)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, &rpcError{Code: -32603, Message: err.Error()}
		}
		source = string(content)
	}
	formatted, err := janet.Format(source)
	if err != nil {
		return nil, &rpcError{Code: -32602, Message: "cannot format Janet source: " + err.Error()}
	}
	if formatted == source {
		return []textEdit{}, nil
	}
	document := janet.Parse(uri, path, source)
	return []textEdit{{
		Range:   document.Range(0, len(source)),
		NewText: formatted,
	}}, nil
}

func (server *Server) documentDiagnostics(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var params struct {
		TextDocument textDocumentIdentifier `json:"textDocument"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, invalidParams(err)
	}
	index, responseError := server.buildIndex(ctx)
	if responseError != nil {
		return nil, responseError
	}
	document := index.Document(params.TextDocument.URI)
	if document == nil {
		return documentDiagnosticReport{Kind: "full", Items: []diagnostic{}}, nil
	}
	items := mergeDiagnostics(diagnostics(document), server.compileDiagnostics(ctx, document))
	return documentDiagnosticReport{Kind: "full", Items: items}, nil
}

func (server *Server) positionRequest(ctx context.Context, raw json.RawMessage) (documentPositionParams, *janet.Index, *janet.Occurrence, *rpcError) {
	var params documentPositionParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return params, nil, nil, invalidParams(err)
	}
	index, responseError := server.buildIndex(ctx)
	if responseError != nil {
		return params, nil, nil, responseError
	}
	document := index.Document(params.TextDocument.URI)
	if document == nil {
		return params, index, nil, nil
	}
	occurrence, ok := index.OccurrenceAt(document.URI, document.Offset(params.Position))
	if !ok {
		return params, index, nil, nil
	}
	return params, index, &occurrence, nil
}

func diagnostics(document *janet.Document) []diagnostic {
	items := make([]diagnostic, 0, len(document.Diagnostics))
	for _, item := range document.Diagnostics {
		items = append(items, diagnostic{
			Range: document.Range(item.Start, item.End), Severity: 1,
			Source: "janetpls", Message: item.Message,
		})
	}
	return items
}

func canonicalURI(uri string) string {
	path, err := janet.URIToPath(uri)
	if err != nil {
		return uri
	}
	return janet.PathToURI(path)
}

func invalidParams(err error) *rpcError {
	return &rpcError{Code: -32602, Message: "invalid params: " + err.Error()}
}

func (server *Server) cancelAll() {
	server.cancellationsMu.Lock()
	defer server.cancellationsMu.Unlock()
	for _, cancel := range server.cancellations {
		cancel()
	}
}
