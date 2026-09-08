package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jhaton/janet-lsp/internal/janet"
)

func TestServerRunInitializeShutdownExit(t *testing.T) {
	root := t.TempDir()
	input := frameJSON(t, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"rootUri": janet.PathToURI(root)},
	}) + frameJSON(t, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "shutdown", "params": map[string]any{},
	}) + frameJSON(t, map[string]any{
		"jsonrpc": "2.0", "method": "exit",
	})
	var output bytes.Buffer
	server, err := NewServer(strings.NewReader(input), &output, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	reader := bufio.NewReader(&output)
	initialize := readFramedJSON(t, reader)
	result := initialize["result"].(map[string]any)
	capabilities := result["capabilities"].(map[string]any)
	if capabilities["positionEncoding"] != "utf-16" ||
		capabilities["referencesProvider"] != true ||
		capabilities["documentFormattingProvider"] != true {
		t.Fatalf("initialize capabilities = %#v", capabilities)
	}
	shutdown := readFramedJSON(t, reader)
	if result, exists := shutdown["result"]; !exists || result != nil {
		t.Fatalf("shutdown result = %#v, exists = %v", result, exists)
	}
}

func TestServerNavigationRenameAndDiagnostics(t *testing.T) {
	root := t.TempDir()
	modelPath := writeLSPFixture(t, root, "model.janet", `(defn target "Target docs." [value] value)
(target 1)
`)
	consumerPath := writeLSPFixture(t, root, "consumer.janet", `(import ./model)
(model/target 2)
`)
	var output bytes.Buffer
	server, err := NewServer(strings.NewReader(""), &output, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	initializeParams := mustJSON(t, initializeParams{RootURI: janet.PathToURI(root)})
	if _, responseError := server.handleRequest(context.Background(), "initialize", initializeParams); responseError != nil {
		t.Fatalf("initialize: %#v", responseError)
	}

	modelURI := janet.PathToURI(modelPath)
	consumerURI := janet.PathToURI(consumerPath)
	model := janet.Parse(modelURI, modelPath, string(mustRead(t, modelPath)))
	declaration := strings.Index(model.Source, "target")
	params := documentPositionParams{
		TextDocument: textDocumentIdentifier{URI: modelURI},
		Position:     model.Position(declaration),
	}

	definitionResult, responseError := server.handleRequest(context.Background(), "textDocument/definition", mustJSON(t, params))
	if responseError != nil {
		t.Fatalf("definition: %#v", responseError)
	}
	definition := definitionResult.(location)
	if definition.URI != modelURI || definition.Range.Start != model.Position(declaration) {
		t.Fatalf("definition = %#v", definition)
	}

	references := referenceParams{TextDocument: params.TextDocument, Position: params.Position}
	references.Context.IncludeDeclaration = true
	referenceResult, responseError := server.handleRequest(context.Background(), "textDocument/references", mustJSON(t, references))
	if responseError != nil {
		t.Fatalf("references: %#v", responseError)
	}
	if got := len(referenceResult.([]location)); got != 3 {
		t.Fatalf("references = %d, want declaration and two calls", got)
	}

	renameResult, responseError := server.handleRequest(context.Background(), "textDocument/rename", mustJSON(t, renameParams{
		TextDocument: params.TextDocument, Position: params.Position, NewName: "renamed",
	}))
	if responseError != nil {
		t.Fatalf("rename: %#v", responseError)
	}
	changes := renameResult.(workspaceEdit).Changes
	if len(changes[modelURI]) != 2 || len(changes[consumerURI]) != 1 {
		t.Fatalf("rename changes = %#v", changes)
	}

	hoverResult, responseError := server.handleRequest(context.Background(), "textDocument/hover", mustJSON(t, params))
	if responseError != nil || !strings.Contains(hoverResult.(hover).Contents.Value, "Target docs.") {
		t.Fatalf("hover = %#v, error = %#v", hoverResult, responseError)
	}

	broken := "(def broken [\n"
	server.handleNotification(context.Background(), rpcMessage{Method: "textDocument/didOpen", Params: mustJSON(t, didOpenParams{
		TextDocument: textDocumentItem{URI: modelURI, LanguageID: "janet", Version: 2, Text: broken},
	})})
	if output.Len() != 0 {
		t.Fatalf("didOpen emitted push diagnostics while pull diagnostics are enabled: %q", output.String())
	}
	diagnosticResult, responseError := server.handleRequest(context.Background(), "textDocument/diagnostic", mustJSON(t, struct {
		TextDocument textDocumentIdentifier `json:"textDocument"`
	}{TextDocument: textDocumentIdentifier{URI: modelURI}}))
	if responseError != nil || len(diagnosticResult.(documentDiagnosticReport).Items) == 0 {
		t.Fatalf("diagnostics = %#v, error = %#v", diagnosticResult, responseError)
	}
}

func TestIncompleteDefinitionSupportsDiagnosticsAndSymbols(t *testing.T) {
	root := t.TempDir()
	path := writeLSPFixture(t, root, "incomplete.janet", "(defn")
	uri := janet.PathToURI(path)
	server, err := NewServer(strings.NewReader(""), io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if _, responseError := server.handleRequest(context.Background(), "initialize", mustJSON(t, initializeParams{
		RootURI: janet.PathToURI(root),
	})); responseError != nil {
		t.Fatalf("initialize: %#v", responseError)
	}

	params := mustJSON(t, struct {
		TextDocument textDocumentIdentifier `json:"textDocument"`
	}{TextDocument: textDocumentIdentifier{URI: uri}})
	diagnostics, responseError := server.handleRequest(context.Background(), "textDocument/diagnostic", params)
	if responseError != nil {
		t.Fatalf("diagnostics: %#v", responseError)
	}
	if len(diagnostics.(documentDiagnosticReport).Items) == 0 {
		t.Fatal("diagnostics = empty, want incomplete-form diagnostic")
	}
	symbols, responseError := server.handleRequest(context.Background(), "textDocument/documentSymbol", params)
	if responseError != nil {
		t.Fatalf("document symbols: %#v", responseError)
	}
	if got := len(symbols.([]documentSymbol)); got != 0 {
		t.Fatalf("document symbols = %d, want 0", got)
	}
}
func TestCancelNotificationCancelsPendingRequest(t *testing.T) {
	server, err := NewServer(strings.NewReader(""), io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	requestContext, cancel := context.WithCancel(context.Background())
	server.cancellations["17"] = cancel
	server.handleNotification(context.Background(), rpcMessage{
		Method: "$/cancelRequest",
		Params: mustJSON(t, map[string]any{"id": 17}),
	})
	select {
	case <-requestContext.Done():
	default:
		t.Fatal("pending request context was not cancelled")
	}
}

func frameJSON(t *testing.T, value any) string {
	t.Helper()
	body := mustJSON(t, value)
	return fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func readFramedJSON(t *testing.T, input io.Reader) map[string]any {
	t.Helper()
	reader, ok := input.(*bufio.Reader)
	if !ok {
		reader = bufio.NewReader(input)
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	lengthText := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
	length, err := strconv.Atoi(lengthText)
	if err != nil {
		t.Fatal(err)
	}
	blank, err := reader.ReadString('\n')
	if err != nil || strings.TrimSpace(blank) != "" {
		t.Fatalf("invalid framing separator %q: %v", blank, err)
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(reader, body); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func writeLSPFixture(t *testing.T, root, name, source string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
