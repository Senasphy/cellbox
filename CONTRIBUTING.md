# Contributing

Thanks for helping improve Cellbox. Keep changes focused and preserve the Docker and native runtime capability boundaries.

## Local workflow

Use Go 1.23 or newer and Python 3.11 or newer. Node is needed only for dashboard changes. The service can use Docker Engine or the Linux native runtime; native runtime work requires delegated cgroup v2 controllers, user namespaces, overlayfs, and seccomp.

```sh
make build
go test ./...
make lint
```

For dashboard changes, from `web/` run `npm ci` and `npm run build`, then from the repository root run `python3 scripts/embed_dashboard.py`. For live Docker acceptance, use `make test-integration` only when a Docker daemon is already available. Do not change a machine's Docker service or boot configuration as part of development.

## Change expectations

- Keep commits scoped to one behavior or closely related set of files.
- Add or update focused tests for behavior changes; report checks that could not be run and why.
- Keep `api/openapi.yaml` and the generated dashboard schema in sync when changing the HTTP API.
- Document security boundaries and unsupported runtime capabilities rather than silently degrading behavior.
- Never commit credentials, API keys, user data, or generated local state.

## Reporting issues

For security vulnerabilities, follow the private reporting instructions in [SECURITY.md](SECURITY.md). For other bugs, include the Cellbox version, runtime backend, relevant configuration with secrets removed, and the smallest useful reproduction.
