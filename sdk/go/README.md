# Go client

This directory is importable as `github.com/senasphy/cellbox/sdk/go`. It is a small `net/http` wrapper for session creation, lookup, deletion, exec, resumable streamed exec output, paginated file listing, file reads and writes, snapshots, and forks. `ExecStream` returns the exec ID; if its connection ends early, `ResumeExecStream` accepts that ID and the last output callback cursor to continue with `Last-Event-ID`. It deliberately has no additional transport dependency.
