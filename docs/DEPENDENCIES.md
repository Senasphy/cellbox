# Dependencies

| Dependency | Purpose | Reason |
| --- | --- | --- |
| `github.com/spf13/cobra` | CLI command tree | Widely used Go CLI library specified by the build plan. |
| `gopkg.in/yaml.v3` | Strict YAML configuration | Small YAML parser with known-field validation. |
| `modernc.org/sqlite` | SQLite driver | Pure Go SQLite so release builds can disable CGO. |
| `github.com/getkin/kin-openapi` | OpenAPI 3 document and response validation in API contract tests | The specification requires responses to be checked against the API contract; this library parses OpenAPI 3 and validates real handler responses. |

The Docker backend currently speaks the Engine HTTP API over a Unix socket with the standard library; this keeps the bootstrap backend small and avoids linking a broad Docker SDK into the service.

Transitive dependencies are recorded in `go.mod` and `go.sum`.
