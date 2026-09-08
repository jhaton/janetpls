package lsp

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func TestConnectionReadsFramedMessage(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":7,"method":"shutdown"}`
	input := "Content-Type: application/vscode-jsonrpc; charset=utf-8\r\nContent-Length: " + jsonLength(body) + "\r\n\r\n" + body
	message, err := newConnection(strings.NewReader(input), &bytes.Buffer{}).read()
	if err != nil {
		t.Fatal(err)
	}
	if message.Method != "shutdown" || string(message.ID) != "7" {
		t.Fatalf("message = %#v", message)
	}
}

func TestConnectionWritesNullSuccessResult(t *testing.T) {
	var output bytes.Buffer
	connection := newConnection(strings.NewReader(""), &output)
	if err := connection.write(rpcMessage{ID: json.RawMessage("1"), Result: nil}); err != nil {
		t.Fatal(err)
	}
	message := readFramedJSON(t, &output)
	result, exists := message["result"]
	if !exists || result != nil {
		t.Fatalf("response result = %#v, exists = %v; want explicit null", result, exists)
	}
	if _, exists := message["method"]; exists {
		t.Fatalf("response unexpectedly contains method: %#v", message)
	}
}

func jsonLength(value string) string {
	return strconv.Itoa(len(value))
}
