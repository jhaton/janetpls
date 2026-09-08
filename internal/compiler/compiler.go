package compiler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// Request is one isolated Janet compilation.
type Request struct {
	Path   string `json:"path"`
	Source string `json:"source"`
}

// Diagnostic is a zero-based source position reported by libjanet.
type Diagnostic struct {
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Message string `json:"message"`
}

// Response is the complete output from one compiler helper process.
type Response struct {
	Diagnostics []Diagnostic `json:"diagnostics"`
	Error       string       `json:"error,omitempty"`
}

// Check invokes an isolated compiler helper. A helper failure is returned to
// the caller so the language server can retain its pure-Go diagnostics.
func Check(ctx context.Context, command []string, root string, request Request) ([]Diagnostic, error) {
	if len(command) == 0 || command[0] == "" {
		return nil, errors.New("compiler helper is unavailable")
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encode compiler request: %w", err)
	}
	process := exec.CommandContext(ctx, command[0], command[1:]...)
	process.Dir = root
	process.Stdin = bytes.NewReader(payload)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	process.Stdout = &stdout
	process.Stderr = &stderr
	if err := process.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return nil, fmt.Errorf("compiler helper: %w: %s", err, detail)
		}
		return nil, fmt.Errorf("compiler helper: %w", err)
	}

	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	var response Response
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("decode compiler response: %w", err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return nil, err
	}
	if response.Error != "" {
		return nil, errors.New(response.Error)
	}
	return response.Diagnostics, nil
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("decode compiler response: trailing JSON value")
		}
		return fmt.Errorf("decode compiler response: %w", err)
	}
	return nil
}
