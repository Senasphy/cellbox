# Examples

- `fix-loop.py --fake-model` runs the deterministic offline repair loop used by CI. For a hosted repair model, set `OPENAI_API_KEY` and optionally `OPENAI_BASE_URL` / `OPENAI_MODEL`.
- `hosted-model.py --fake-model` runs its deterministic offline tool call; otherwise it uses an OpenAI-compatible chat-completions API. Configure `CELLBOX_URL`, `CELLBOX_KEY`, `OPENAI_API_KEY`, and optionally `OPENAI_BASE_URL` / `OPENAI_MODEL`.
- `ollama.py --fake-model` runs its deterministic offline tool call; otherwise it uses a local Ollama endpoint. Configure `CELLBOX_URL`, `CELLBOX_KEY`, `OLLAMA_URL`, and `OLLAMA_MODEL`.
- `eval-runner.py --fake-model` executes a deterministic task set concurrently in isolated sessions and writes a JSON summary.

The model examples use the bounded dispatcher in `sdk/tools/cellbox_tools.py`. Snapshot and fork tool declarations are forward-looking because those API routes arrive in Phase 6.
