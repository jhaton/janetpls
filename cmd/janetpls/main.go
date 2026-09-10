package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/jhaton/janetpls/internal/lsp"
)

func main() {
	showVersion := flag.Bool("version", false, "print the server version")
	flag.BoolVar(showVersion, "v", false, "print the server version")
	flag.Parse()

	if *showVersion {
		fmt.Printf("Janet PLS v%s\n", lsp.Version)
		return
	}

	server, err := lsp.NewServer(os.Stdin, os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "janetpls: %v\n", err)
		os.Exit(1)
	}
	if err := server.Run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "janetpls: %v\n", err)
		os.Exit(1)
	}
}
