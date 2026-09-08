# Janet LSP

A standalone [Language Server Protocol](https://microsoft.github.io/language-server-protocol/) server for [Janet](https://janet-lang.org), implemented in Go.

The server parses Janet source without evaluating it. It therefore remains useful while a buffer is incomplete and never executes workspace code as part of diagnostics or indexing.

## Features

- Tolerant Janet lexer and delimiter parser, including mutable containers, comments, escaped strings, and long backtick strings
- Push and pull syntax diagnostics
- Go to definition for top-level and lexical bindings
- Workspace references and scope-aware rename
- Default `import` prefixes, `:as` aliases, `:prefix` aliases, and `use`
- Completion for visible local, module, and imported symbols
- Hover documentation and signature help from source definitions
- Document symbols
- UTF-16 LSP position handling
- Full-document open-buffer overlays and request cancellation
- Git-bounded workspace discovery with a recursive fallback outside Git repositories

The server intentionally does not advertise formatting. Diagnostics currently cover source structure, not Janet compile-time or runtime errors. Dynamic bindings introduced by arbitrary macros cannot be inferred statically.

## Build

The repository pins its Go toolchain with [mise](https://mise.jdx.dev/):

```sh
git clone https://github.com/jhaton/janet-lsp.git
cd janet-lsp
mise install
mise exec -- make check
```

The binary is written to `bin/janet-lsp`. Janet itself is not required to build or run the language server.

To install through Go instead:

```sh
go install github.com/jhaton/janet-lsp/cmd/janet-lsp@latest
```

Confirm the installed binary:

```sh
janet-lsp --version
```

## Editor configuration

Configure an LSP client to start `janet-lsp` over standard input and output for `*.janet` files, with the project directory as the workspace root.

Neovim 0.11 example:

```lua
vim.lsp.config("janet_lsp", {
  cmd = { "janet-lsp" },
  filetypes = { "janet" },
  root_markers = { "project.janet", ".git" },
})
vim.lsp.enable("janet_lsp")
```

Helix example:

```toml
[language-server.janet-lsp]
command = "janet-lsp"

[[language]]
name = "janet"
language-servers = ["janet-lsp"]
```

## Architecture

- `cmd/janet-lsp`: CLI and stdio process lifecycle
- `internal/lsp`: JSON-RPC framing, LSP request dispatch, open-document state, and wire types
- `internal/janet`: tolerant syntax model, UTF-16 position conversion, lexical scopes, imports, and workspace index

Each language request builds a deterministic index from Git-tracked and unignored Janet files plus the current in-memory buffers. This favors correctness under external file changes and keeps server state small. Requests run independently and honor `$/cancelRequest`.

## Development

```sh
mise exec -- make test   # unit and protocol integration tests
mise exec -- make race   # race detector
mise exec -- make build  # bin/janet-lsp
mise exec -- make check  # all of the above plus a version smoke test
```

## Prior art

This project originated as a hard fork of [JohnDoneth/janet-language-server](https://github.com/JohnDoneth/janet-language-server). The pre-Go implementation remains available through repository history.
