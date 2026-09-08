package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/jhaton/janet-lsp/internal/lsp"
)

func main() {
	showVersion := flag.Bool("version", false, "print the server version")
	flag.BoolVar(showVersion, "v", false, "print the server version")
	flag.Parse()

	if *showVersion {
		fmt.Printf("Janet LSP v%s\n", lsp.Version)
		return
	}

	server, err := lsp.NewServer(os.Stdin, os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "janet-lsp: %v\n", err)
		os.Exit(1)
	}
	if err := server.Run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "janet-lsp: %v\n", err)
		os.Exit(1)
	}
}
