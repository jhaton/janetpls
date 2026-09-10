package lsp

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/jhaton/janetpls/internal/janet"
)

func TestFormattingUsesOpenDocumentAndReturnsFullEdit(t *testing.T) {
	root := t.TempDir()
	path := writeLSPFixture(t, root, "model.janet", "(def disk-value 1)\n")
	uri := janet.PathToURI(path)
	var output bytes.Buffer
	server, err := NewServer(strings.NewReader(""), &output, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	openSource := "(def   memory-value\n1 )"
	server.handleNotification(context.Background(), rpcMessage{
		Method: "textDocument/didOpen",
		Params: mustJSON(t, didOpenParams{TextDocument: textDocumentItem{
			URI: uri, LanguageID: "janet", Version: 1, Text: openSource,
		}}),
	})
	result, responseError := server.handleRequest(
		context.Background(),
		"textDocument/formatting",
		mustJSON(t, documentFormattingParams{TextDocument: textDocumentIdentifier{URI: uri}}),
	)
	if responseError != nil {
		t.Fatalf("formatting: %#v", responseError)
	}
	edits := result.([]textEdit)
	if len(edits) != 1 {
		t.Fatalf("formatting edits = %#v", edits)
	}
	if edits[0].NewText != "(def memory-value\n  1)\n" {
		t.Fatalf("formatted source = %q", edits[0].NewText)
	}
	document := janet.Parse(uri, path, openSource)
	if edits[0].Range != document.Range(0, len(openSource)) {
		t.Fatalf("edit range = %#v, want complete open document", edits[0].Range)
	}
}

func TestFormattingRejectsMalformedSource(t *testing.T) {
	root := t.TempDir()
	path := writeLSPFixture(t, root, "broken.janet", "(def value [1 2)\n")
	server, err := NewServer(strings.NewReader(""), io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	result, responseError := server.handleRequest(
		context.Background(),
		"textDocument/formatting",
		mustJSON(t, documentFormattingParams{
			TextDocument: textDocumentIdentifier{URI: janet.PathToURI(path)},
		}),
	)
	if result != nil || responseError == nil || responseError.Code != -32602 {
		t.Fatalf("result = %#v, error = %#v", result, responseError)
	}
}
