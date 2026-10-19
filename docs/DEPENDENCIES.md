# Dependencies

| Dependency | Purpose | Reason |
| --- | --- | --- |
| `github.com/spf13/cobra` | CLI command tree | Widely used Go CLI library specified by the build plan. |
| `gopkg.in/yaml.v3` | Strict YAML configuration | Small YAML parser with known-field validation. |
| `modernc.org/sqlite` | SQLite driver | Pure Go SQLite so release builds can disable CGO. |
| `github.com/getkin/kin-openapi` | OpenAPI 3 document and response validation in API contract tests | The specification requires responses to be checked against the API contract; this library parses OpenAPI 3 and validates real handler responses. |
| `github.com/modelcontextprotocol/go-sdk` | MCP server over stdio and Streamable HTTP | Official Go SDK; pinned to v1.3.1 to retain the Go 1.23 release toolchain. |
| `httpx` | Python sync and async SDK transport | Mature HTTP client with streaming and async support. |
| `Next.js`, `React` | Static dashboard source and export | Dashboard is exported at build time and embedded into the Go binary. |
| `typescript`, `tsup` | TypeScript SDK type checking and ESM/CJS bundles | Emits dual module formats with declarations. |

The Docker backend currently speaks the Engine HTTP API over a Unix socket with the standard library; this keeps the bootstrap backend small and avoids linking a broad Docker SDK into the service.

Transitive dependencies are recorded in `go.mod` and `go.sum`.
