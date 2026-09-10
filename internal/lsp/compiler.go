package lsp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	compilerapi "github.com/jhaton/janetpls/internal/compiler"
	"github.com/jhaton/janetpls/internal/janet"
)

const defaultCompilerTimeout = 2 * time.Second

func (server *Server) configureCompiler(options initializationOptions) {
	server.compilerEnabled = options.CompilerDiagnostics
	server.compilerTimeout = defaultCompilerTimeout
	if !server.compilerEnabled {
		return
	}
	command, err := compilerCommand(options.CompilerPath, server.root)
	if err != nil {
		server.logCompilerFailure(err)
		return
	}
	server.compilerCommand = command
}

func compilerCommand(explicit, root string) ([]string, error) {
	if explicit != "" {
		path := explicit
		if !filepath.IsAbs(path) && filepath.Dir(path) != "." {
			path = filepath.Join(root, path)
		}
		resolved, err := executablePath(path)
		if err != nil {
			return nil, fmt.Errorf("resolve configured compiler helper: %w", err)
		}
		return []string{resolved}, nil
	}
	if executable, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(executable), "janetpls-compiler")
		if resolved, err := executablePath(candidate); err == nil {
			return []string{resolved}, nil
		}
	}
	resolved, err := exec.LookPath("janetpls-compiler")
	if err != nil {
		return nil, errors.New("compiler diagnostics requested but janetpls-compiler was not found")
	}
	return []string{resolved}, nil
}

func executablePath(path string) (string, error) {
	if filepath.Dir(path) == "." {
		return exec.LookPath(path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("%s is not executable", path)
	}
	return path, nil
}

func (server *Server) compileDiagnostics(ctx context.Context, document *janet.Document) []diagnostic {
	if !server.compilerEnabled || len(server.compilerCommand) == 0 || len(document.Diagnostics) != 0 {
		return nil
	}
	root := server.workspaceRoot()
	checkContext, cancel := context.WithTimeout(ctx, server.compilerTimeout)
	defer cancel()
	items, err := compilerapi.Check(checkContext, server.compilerCommand, root, compilerapi.Request{
		Path: document.Path, Source: document.Source,
	})
	if err != nil {
		server.logCompilerFailure(err)
		return nil
	}
	result := make([]diagnostic, 0, len(items))
	for _, item := range items {
		startOffset := document.Offset(janet.Position{Line: max(item.Line, 0), Character: max(item.Column, 0)})
		endOffset := min(startOffset+1, len(document.Source))
		result = append(result, diagnostic{
			Range:    document.Range(startOffset, endOffset),
			Severity: 1,
			Source:   "janet compiler",
			Message:  item.Message,
		})
	}
	return result
}

func mergeDiagnostics(groups ...[]diagnostic) []diagnostic {
	count := 0
	for _, group := range groups {
		count += len(group)
	}
	result := make([]diagnostic, 0, count)
	seen := make(map[string]struct{}, count)
	for _, group := range groups {
		for _, item := range group {
			key := strconv.Itoa(item.Range.Start.Line) + ":" + strconv.Itoa(item.Range.Start.Character) + ":" + item.Message
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, item)
		}
	}
	return result
}

func (server *Server) workspaceRoot() string {
	server.rootMu.RLock()
	defer server.rootMu.RUnlock()
	return server.root
}

func (server *Server) logCompilerFailure(err error) {
	server.compilerFailureOnce.Do(func() {
		fmt.Fprintf(server.stderr, "compiler diagnostics unavailable; continuing with Go analysis: %v\n", err)
	})
}
