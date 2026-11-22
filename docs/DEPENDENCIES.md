# Direct dependencies

Direct dependencies are listed with their role in the project and the reason they are used. Version constraints live in the corresponding Go, Python, or npm manifest; transitive Go versions are recorded in `go.mod` and `go.sum`, and npm resolves from its lockfile.

## Go service and CLI

| Dependency | Purpose | Reason |
| --- | --- | --- |
| `github.com/elastic/go-seccomp-bpf` | Native runtime syscall filters | Builds and installs the synchronized seccomp filters used by init and workload helpers. |
| `github.com/getkin/kin-openapi` | API contract validation | Parses the OpenAPI document and validates handler requests and responses in contract tests. |
| `github.com/google/go-containerregistry` | Native OCI image operations | Pulls, authenticates, resolves, and unpacks OCI images without shelling out to a container CLI. |
| `github.com/gorilla/websocket` | Terminal WebSocket transport | Provides WebSocket upgrade and frame handling for API terminals and the CLI shell. |
| `github.com/modelcontextprotocol/go-sdk` | MCP stdio and Streamable HTTP | Implements the standard MCP server transports and protocol. |
| `github.com/spf13/cobra` | CLI command tree | Provides command parsing, flags, and help for the `cellbox` CLI. |
| `golang.org/x/net` | IDNA host normalization | Normalizes internationalized egress policy names before authorization and DNS checks. |
| `golang.org/x/sync` | OCI image request coalescing | `singleflight` prevents concurrent duplicate image pulls and flatten operations. |
| `golang.org/x/sys` | Linux runtime syscalls | Exposes cgroups, namespaces, seccomp, `openat2`, PTY, and process-control interfaces. |
| `gopkg.in/yaml.v3` | Service configuration | Parses YAML while supporting strict known-field validation. |
| `modernc.org/sqlite` | SQLite persistence | Provides a pure-Go SQLite driver so release builds can disable CGO. |

The Docker backend speaks the Engine HTTP API over its Unix socket with Go's standard library. This avoids adding a broad Docker SDK to the service.

## SDKs and tools

| Dependency | Purpose | Reason |
| --- | --- | --- |
| Python `httpx` | Sync and async Python SDK transport | Supports ordinary HTTP, streaming responses, and async clients with one API. |
| Python `hatchling` | Python package build backend | Builds the distributable SDK wheel from `sdk/python/src`. |
| Python `pytest` (test extra) | Python SDK tests | Runs isolated unit tests and explicitly marked live-service integration tests. |
| TypeScript `typescript` | Type checking and declarations | Checks the SDK and emits its public type declarations. |
| TypeScript `tsup` | TypeScript SDK packaging | Builds the SDK's ESM and CommonJS entry points. |
| TypeScript `vitest` | TypeScript SDK tests | Runs the SDK unit and regression suite. |

## Dashboard and browser verification

| Dependency | Purpose | Reason |
| --- | --- | --- |
| `next`, `react`, `react-dom` | Dashboard UI and static export | Builds the browser application as static files that can be embedded in the service binary. |
| `openapi-fetch`, `openapi-typescript` | Typed API client and generated types | Keeps dashboard requests aligned with the checked-in OpenAPI contract. |
| `@tanstack/react-query` | Server-state cache | Shares loading, cancellation, refresh, and invalidation behavior across dashboard views. |
| `@xterm/xterm`, `@xterm/addon-fit`, `@xterm/addon-webgl` | Terminal and replay display | Renders interactive PTY and recorded command output with fit and optional WebGL rendering. |
| `codemirror`, `@codemirror/state` | Read-only file viewer | Provides the dashboard's code-oriented text editor and immutable viewer state. |
| `uplot` | Metrics charts | Renders compact CPU, memory, and I/O charts without a larger charting framework. |
| `@playwright/test` | Browser workflows | Exercises end-to-end dashboard behavior in a real browser. |
| `@axe-core/playwright` | Automated accessibility checks | Runs axe accessibility scans from the browser test suite. |
| `typescript`, `@types/react`, `@types/node` | Dashboard type checking | Supplies the compiler and declarations for application and Node APIs. |

The dashboard dependency lockfile is `web/package-lock.json`. The Go module files pin Go dependencies; `sdk/python/pyproject.toml` and `sdk/typescript/package.json` declare SDK dependencies.
