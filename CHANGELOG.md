# Changelog

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
