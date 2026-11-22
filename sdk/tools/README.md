# Model tool schemas

`openai.json` and `anthropic.json` contain equivalent function/tool definitions. Dispatch helpers are in `cellbox_tools.py`. Output from commands is clipped independently to 16 KiB for stdout and stderr, retaining the head and tail and inserting an omitted-bytes marker. Snapshot and fork dispatch through the Python SDK; fork can use an existing `snapshot_id` or snapshot and fork the current session in one call.
