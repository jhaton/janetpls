//go:build cgo && libjanet

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/jhaton/janetpls/internal/compiler"
)

const maximumRequestBytes = 64 << 20

func main() {
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, maximumRequestBytes))
	var request compiler.Request
	if err := decoder.Decode(&request); err != nil {
		writeResponse(compiler.Response{Error: fmt.Sprintf("decode compiler request: %v", err)})
		return
	}
	response, err := compileSource(request)
	if err != nil {
		response = compiler.Response{Error: err.Error()}
	}
	writeResponse(response)
}

func writeResponse(response compiler.Response) {
	if err := json.NewEncoder(os.Stdout).Encode(response); err != nil {
		fmt.Fprintf(os.Stderr, "encode compiler response: %v\n", err)
		os.Exit(1)
	}
}
