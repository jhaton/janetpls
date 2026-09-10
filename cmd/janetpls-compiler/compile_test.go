//go:build cgo && libjanet

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jhaton/janetpls/internal/compiler"
)

func TestCompileSourceReportsUnknownSymbol(t *testing.T) {
	response, err := compileSource(compiler.Request{
		Path:   "model.janet",
		Source: "\n\n(efn check-doors! [game] game)\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v, want one compile error", response.Diagnostics)
	}
	diagnostic := response.Diagnostics[0]
	if diagnostic.Line != 2 || diagnostic.Column != 0 || diagnostic.Message != "unknown symbol efn" {
		t.Fatalf("diagnostic = %#v", diagnostic)
	}
}

func TestCompileSourceAcceptsSequentialDefinitions(t *testing.T) {
	response, err := compileSource(compiler.Request{
		Path: "model.janet",
		Source: `(def answer 1)
(defn add-answer [value]
  (+ value answer))
(add-answer 2)
`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want valid compilation", response.Diagnostics)
	}
}

func TestCompileSourceResolvesProjectDependency(t *testing.T) {
	modulePath := t.TempDir()
	moduleSource := []byte("(defn load-texture [path] path)\n")
	if err := os.WriteFile(filepath.Join(modulePath, "textures.janet"), moduleSource, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JANET_PATH", modulePath)

	response, err := compileSource(compiler.Request{
		Path: "assets.janet",
		Source: `(use textures)
(map load-texture ["texture.png"])
`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want project dependency to resolve", response.Diagnostics)
	}
}
