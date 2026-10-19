# Examples

- `fix-loop.py --fake-model` runs the deterministic offline repair loop used by CI.
- `hosted-model.py` runs an OpenAI-compatible chat-completions tool loop; configure `CELLBOX_URL`, `CELLBOX_KEY`, `OPENAI_API_KEY`, and optionally `OPENAI_BASE_URL` / `OPENAI_MODEL`.
- `ollama.py` runs the same tool loop using a local Ollama endpoint; configure `CELLBOX_URL`, `CELLBOX_KEY`, `OLLAMA_URL`, and `OLLAMA_MODEL`.
- `eval-runner.py` executes a small task set concurrently in isolated sessions and writes a JSON summary.

The model examples use the bounded dispatcher in `sdk/tools/cellbox_tools.py`. Snapshot and fork tool declarations are forward-looking because those API routes arrive in Phase 6.
