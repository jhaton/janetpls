# Janet PLS

A standalone [Language Server Protocol](https://microsoft.github.io/language-server-protocol/) server for [Janet](https://janet-lang.org), implemented in Go.

The server's core parses Janet source without evaluating it, so formatting and code intelligence remain useful while a buffer is incomplete. Trusted workspaces may opt into compiler diagnostics through an isolated `libjanet` helper; its absence or failure never disables the pure-Go server.

## Features

- Tolerant Janet lexer and delimiter parser, including mutable containers, comments, escaped strings, and long backtick strings
- Pull-based syntax diagnostics, with optional compile-time diagnostics from `libjanet`
- Go to definition for top-level and lexical bindings
- Workspace references and scope-aware rename
- Default `import` prefixes, `:as` aliases, `:prefix` aliases, and `use`
- Completion for visible local, module, and imported symbols
- Hover documentation and signature help from source definitions
- Document symbols
- Deterministic whole-document formatting compatible with `spork/fmt`
- UTF-16 LSP position handling
- Full-document open-buffer overlays and request cancellation
- Git-bounded workspace discovery with a recursive fallback outside Git repositories

Formatting rejects malformed source rather than returning a destructive edit. Without the optional compiler helper, diagnostics cover source structure rather than Janet compile-time or runtime errors. Dynamic bindings introduced by arbitrary macros cannot always be inferred.

## Build

The repository pins its Go toolchain with [mise](https://mise.jdx.dev/):

```sh
git clone https://github.com/jhaton/janetpls.git
cd janetpls
mise install
mise exec -- make check
```

The pure-Go binary is written to `bin/janetpls`. Janet itself is not required to build or run it.

To install through Go instead:

```sh
go install github.com/jhaton/janetpls/cmd/janetpls@latest
```

Confirm the installed binary:

```sh
janetpls --version
```

## Optional compiler diagnostics

Compiler diagnostics catch semantic errors such as `unknown symbol efn`. They run in a separate, one-request helper process so a missing shared library, native crash, timeout, or malformed response cannot take down the language server.

Build and test the helper when the Janet development package is available through `pkg-config`:

```sh
mise exec -- make check-compiler
```

This writes `bin/janetpls-compiler`. Install both binaries into the same directory:

```sh
GOBIN=\"$HOME/.local/bin\" go install github.com/jhaton/janetpls/cmd/janetpls@latest
GOBIN=\"$HOME/.local/bin\" go install -tags libjanet github.com/jhaton/janetpls/cmd/janetpls-compiler@latest
```

Enable the helper with initialization options:

```json
{
  \"compilerDiagnostics\": true
}
```

The server finds `janetpls-compiler` beside `janetpls` and then on `PATH`. Set `compilerPath` in the same object to use an explicit helper executable. Compilation can expand macros and imports, so enable it only for trusted workspaces. Each check has a two-second limit. Helper errors are logged once; the request then returns the pure-Go diagnostics.

## Editor configuration

Configure an LSP client to start `janetpls` over standard input and output for `*.janet` files, with the project directory as the workspace root.

Neovim 0.11 example:

```lua
vim.lsp.config("janetpls", {
  cmd = { "janetpls" },
  filetypes = { "janet" },
  root_markers = { "project.janet", ".git" },
  init_options = {
    compilerDiagnostics = true,
  },
})
vim.lsp.enable("janetpls")
```

Helix example:

```toml
[language-server.janetpls]
command = "janetpls"

[[language]]
name = "janet"
language-servers = ["janetpls"]
```

## Architecture

- `cmd/janetpls`: pure-Go CLI and stdio process lifecycle
- `cmd/janetpls-compiler`: optional cgo/libjanet compiler worker
- `internal/compiler`: isolated worker protocol, process execution, cancellation, and failure handling
- `internal/lsp`: JSON-RPC framing, LSP request dispatch, open-document state, and wire types
- `internal/janet`: tolerant syntax model, deterministic formatter, UTF-16 position conversion, lexical scopes, imports, and workspace index

Each language request builds a deterministic index from Git-tracked and unignored Janet files plus the current in-memory buffers. Compiler checks use the requested open-buffer source. Requests run independently and honor `$/cancelRequest`.

## Development

```sh
mise exec -- make test            # pure-Go unit and protocol integration tests
mise exec -- make race            # pure-Go race detector
mise exec -- make build           # bin/janetpls
mise exec -- make check            # pure-Go checks and version smoke test
mise exec -- make check-compiler   # cgo helper tests and build
```

## Prior art

This project originated as a hard fork of [JohnDoneth/janet-language-server](https://github.com/JohnDoneth/janet-language-server). The formatter algorithm is adapted from [`janet-lang/spork`](https://github.com/janet-lang/spork) under its MIT license. The pre-Go implementation remains available through repository history.
