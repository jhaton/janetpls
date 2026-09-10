package compiler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestCheckExchangesStructuredDiagnostics(t *testing.T) {
	command := []string{os.Args[0], "-test.run=TestCompilerHelperProcess", "--", "success"}
	diagnostics, err := Check(context.Background(), command, t.TempDir(), Request{
		Path: "model.janet", Source: "(efn model [])",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []Diagnostic{{Line: 0, Column: 0, Message: "unknown symbol efn"}}
	if !slices.Equal(diagnostics, want) {
		t.Fatalf("diagnostics = %#v, want %#v", diagnostics, want)
	}
}

func TestCheckUsesLocalJPMModulePath(t *testing.T) {
	root := t.TempDir()
	modulePath := filepath.Join(root, "jpm_tree", "lib")
	if err := os.MkdirAll(modulePath, 0o700); err != nil {
		t.Fatal(err)
	}
	command := []string{
		os.Args[0],
		"-test.run=TestCompilerHelperProcess",
		"--",
		"module-path",
		modulePath,
	}
	if _, err := Check(context.Background(), command, root, Request{}); err != nil {
		t.Fatal(err)
	}
}

func TestCheckReportsHelperFailures(t *testing.T) {
	tests := []struct {
		name    string
		mode    string
		timeout time.Duration
	}{
		{name: "exit", mode: "exit", timeout: time.Second},
		{name: "invalid response", mode: "invalid", timeout: time.Second},
		{name: "timeout", mode: "sleep", timeout: 20 * time.Millisecond},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), test.timeout)
			defer cancel()
			command := []string{os.Args[0], "-test.run=TestCompilerHelperProcess", "--", test.mode}
			if _, err := Check(ctx, command, t.TempDir(), Request{}); err == nil {
				t.Fatal("Check succeeded, want helper failure")
			}
		})
	}
}

func TestCompilerHelperProcess(t *testing.T) {
	separator := slices.Index(os.Args, "--")
	if separator < 0 || separator+1 >= len(os.Args) {
		return
	}
	switch os.Args[separator+1] {
	case "success":
		var request Request
		if err := json.NewDecoder(os.Stdin).Decode(&request); err != nil {
			os.Exit(2)
		}
		if request.Path != "model.janet" || request.Source != "(efn model [])" {
			os.Exit(3)
		}
		_ = json.NewEncoder(os.Stdout).Encode(Response{Diagnostics: []Diagnostic{{
			Line: 0, Column: 0, Message: "unknown symbol efn",
		}}})
		os.Exit(0)
	case "module-path":
		if separator+2 >= len(os.Args) || os.Getenv("JANET_PATH") != os.Args[separator+2] {
			os.Exit(4)
		}
		_ = json.NewEncoder(os.Stdout).Encode(Response{Diagnostics: []Diagnostic{}})
		os.Exit(0)
	case "exit":
		fmt.Fprintln(os.Stderr, "compiler crashed")
		os.Exit(7)
	case "invalid":
		fmt.Fprint(os.Stdout, "not json")
		os.Exit(0)
	case "sleep":
		time.Sleep(time.Minute)
		os.Exit(0)
	}
}
