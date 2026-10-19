# Model tool schemas

`openai.json` and `anthropic.json` contain equivalent function/tool definitions. Dispatch helpers are in `cellbox_tools.py`. Output from commands is clipped independently to 16 KiB for stdout and stderr, retaining the head and tail and inserting an omitted-bytes marker. Snapshot and fork are declared for forward compatibility; the current server returns `not_implemented` for those operations until Phase 6.
