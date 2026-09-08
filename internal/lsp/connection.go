package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

type connection struct {
	reader *bufio.Reader
	writer io.Writer
	mu     sync.Mutex
}

func newConnection(reader io.Reader, writer io.Writer) *connection {
	return &connection{reader: bufio.NewReader(reader), writer: writer}
}

func (connection *connection) read() (rpcMessage, error) {
	contentLength := -1
	for {
		line, err := connection.reader.ReadString('\n')
		if err != nil {
			return rpcMessage{}, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, found := strings.Cut(line, ":")
		if !found || !strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			continue
		}
		length, parseErr := strconv.Atoi(strings.TrimSpace(value))
		if parseErr != nil || length < 0 {
			return rpcMessage{}, fmt.Errorf("invalid Content-Length %q", value)
		}
		contentLength = length
	}
	if contentLength < 0 {
		return rpcMessage{}, fmt.Errorf("missing Content-Length header")
	}
	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(connection.reader, payload); err != nil {
		return rpcMessage{}, err
	}
	var message rpcMessage
	if err := json.Unmarshal(payload, &message); err != nil {
		return rpcMessage{}, fmt.Errorf("decode JSON-RPC message: %w", err)
	}
	return message, nil
}

func (connection *connection) write(message rpcMessage) error {
	wire := map[string]any{"jsonrpc": "2.0"}
	if message.Method != "" {
		wire["method"] = message.Method
		if len(message.Params) > 0 {
			wire["params"] = message.Params
		}
	} else {
		wire["id"] = message.ID
		if message.Error != nil {
			wire["error"] = message.Error
		} else {
			wire["result"] = message.Result
		}
	}
	payload, err := json.Marshal(wire)
	if err != nil {
		return err
	}
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if _, err := fmt.Fprintf(connection.writer, "Content-Length: %d\r\n\r\n", len(payload)); err != nil {
		return err
	}
	_, err = connection.writer.Write(payload)
	return err
}
