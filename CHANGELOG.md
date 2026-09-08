# Changelog


## 0.4.0 - 2026-09-08

- Added opt-in Janet compiler diagnostics through an isolated cgo/libjanet helper process.
- Report compile-time errors such as unknown symbols from open-buffer contents.
- Keep the pure-Go language server functional when the helper is absent, crashes, times out, or returns invalid output.
- Skip compiler checks for syntactically incomplete buffers and deduplicate merged diagnostics.
- Use pull diagnostics exclusively to avoid duplicate diagnostics in clients that support both LSP mechanisms.

## 0.3.0 - 2026-09-08

- Added native, deterministic whole-document Janet formatting.
- Preserve comments, reader macros, mutable containers, strings, and canonical control-form indentation without evaluating source.
- Reject malformed input instead of returning a destructive formatting edit.
- Advertise and implement `textDocument/formatting` using open-document overlays.
- Added formatter and protocol regression coverage.

## 0.2.0 - 2026-09-07

- Reimplemented the language server as a standalone Go binary with no Janet runtime dependency.
- Added a tolerant, non-evaluating Janet parser with UTF-16 position handling.
- Added safe syntax diagnostics for mismatched delimiters and unterminated strings.
- Added scope-aware definitions, references, rename, completion, hover, signature help, and document symbols.
- Resolve default imports, `:as` aliases, `:prefix` aliases, and `use` across the workspace.
- Use Git-tracked and unignored Janet files for bounded indexing, with a recursive fallback.
- Added concurrent JSON-RPC request handling, `$/cancelRequest`, and synchronized responses.
- Added parser, workspace-index, protocol framing, lifecycle, navigation, rename, and diagnostics tests.

## 0.1.0 - 2026-09-07

- Added workspace references and scope-aware rename to the Janet implementation.
- Added default-import and `:as` alias resolution.
- Added Git-bounded workspace indexing and integration tests.
