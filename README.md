# cellbox

Self-hostable disposable sandboxes for AI agents on Linux.

cellbox spins up isolated, resource-capped environments where agents can run untrusted code, edit files, and use a shell without touching your host. It includes an HTTP API, a CLI, a built-in web dashboard, an MCP server, and SDKs for Python, TypeScript, and Go.

## Main features

- **Two runtime backends**: Native Linux isolation (namespaces, cgroups v2, seccomp, dropped capabilities) by default, or Docker (`--backend docker`).
- **Fail-closed egress**: Networking is off by default; when enabled, outbound traffic goes through a per-session egress proxy with allowlists/CIDR blocks.
- **Snapshots, forks & replay**: Checkpoint a session's filesystem, fork new sessions from it, and replay recorded runs or export asciicast v2 recordings.
- **Warm pools**: Keep pre-warmed sandboxes around for common images so `create` stays fast.
- **Web dashboard**: Embedded UI served at `/` with live interactive terminals, a file browser, metrics, egress logs, and session replay.
- **Agent-ready**: Official [Python](sdk/python), [TypeScript](sdk/typescript), and [Go](sdk/go) SDKs, model tool schemas in [sdk/tools](sdk/tools), and a built-in MCP server (`cellbox mcp`).

## Quickstart

Requires Linux and Go 1.23+ (plus cgroup v2 delegation for the native backend, or Docker if using `--backend docker`).

```sh
make build
./bin/cellbox doctor
```

Start the server:

```sh
./bin/cellbox serve
```

On first run, `cellbox serve` creates its SQLite database under `~/.local/share/cellbox`, binds to `127.0.0.1:8080`, and prints a one-time admin API key (`cbx_...`).

Export that key and run a one-off command in a disposable sandbox:

```sh
export CELLBOX_KEY=cbx_...
./bin/cellbox run --image alpine:latest -- sh -c 'echo hello'
```

You can also open `http://127.0.0.1:8080` in your browser and sign in with the same key to use the dashboard.

## Using it

Common CLI commands:

```sh
cellbox sessions ls              # list active sessions
cellbox shell <session-id>       # attach an interactive PTY
cellbox snapshot <session-id>    # snapshot a session's filesystem
cellbox fork <session-id>        # fork a new session from a snapshot
cellbox keys create              # mint another API key
cellbox mcp                      # run the MCP server over stdio (or --transport http)
```

Using the Python SDK:

```python
from cellbox import Client

with Client("http://127.0.0.1:8080", api_key="cbx_...") as client:
    with client.sessions.create("python:3.12-slim", memory_mb=512) as box:
        box.write_file("/work/main.py", "print(2 + 2)\n")
        res = box.exec(["python", "/work/main.py"])
        print(res.stdout, res.exit_code)
```

More examples (including Ollama, OpenAI-compatible models, and eval loops) are in [examples/](examples/).

## Security & caveats

- The native backend shares the host kernel. If you're running hostile multi-tenant workloads, run `cellbox` inside a dedicated VM.
- The Docker backend relies on your Docker daemon's isolation and doesn't enforce per-session disk quotas.
- Don't expose the server beyond localhost without TLS, quotas, and an explicit egress policy.

## Further reading

- [API reference](docs/API.md)
- [Operations & deployment](docs/OPERATIONS.md)
- [Threat model](docs/THREAT_MODEL.md)
- [Security policy](SECURITY.md)

## Contributing

Pull requests and bug reports are welcome. Run `make test` and `make lint` before opening a PR (and `make web` if you touched the dashboard). See [CONTRIBUTING.md](CONTRIBUTING.md) for the full dev workflow and expectations.

## License

Apache 2.0 — see [LICENSE](LICENSE).
