#!/usr/bin/env bash
set -euo pipefail

cellbox_bin=${CELLBOX_BIN:-./bin/cellbox}
cellbox_bin=$(realpath "$cellbox_bin")
repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
work_dir=$(mktemp -d)
port=$(python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
)
base="http://127.0.0.1:${port}"
server_pid=
session_id=

cleanup() {
	if [[ -n "$session_id" ]]; then
		curl -fsS -X DELETE "$base/v1/sessions/$session_id" \
			-H "Authorization: Bearer $api_key" >/dev/null || true
	fi
	if [[ -n "$server_pid" ]]; then
		kill "$server_pid" 2>/dev/null || true
		wait "$server_pid" 2>/dev/null || true
	fi
	rm -rf "$work_dir"
}
trap cleanup EXIT

"$cellbox_bin" serve --backend docker --data-dir "$work_dir/data" --listen "127.0.0.1:$port" >"$work_dir/server.log" 2>&1 &
server_pid=$!
for _ in $(seq 1 100); do
	if curl -fsS "$base/healthz" >/dev/null 2>&1; then
		break
	fi
	sleep 0.1
done
if ! curl -fsS "$base/healthz" >/dev/null; then
	cat "$work_dir/server.log" >&2
	exit 1
fi

api_key=$("$cellbox_bin" keys create docker-walkthrough --data-dir "$work_dir/data")
session_json=$(curl -fsS "$base/v1/sessions" \
	-H "Authorization: Bearer $api_key" \
	-H 'Content-Type: application/json' \
	-d '{"image":"alpine:latest","network_policy":{"mode":"none"}}')
session_id=$(jq -er '.id' <<<"$session_json")

exec_json=$(curl -fsS -X POST "$base/v1/sessions/$session_id/execs" \
	-H "Authorization: Bearer $api_key" \
	-H 'Content-Type: application/json' \
	-d '{"argv":["/bin/sh","-c","printf cellbox-stream-ok"]}')
exec_id=$(jq -er '.id' <<<"$exec_json")
curl -fsSN "$base/v1/sessions/$session_id/execs/$exec_id/stream" \
	-H "Authorization: Bearer $api_key" -o "$work_dir/events"
grep -q 'event: exit' "$work_dir/events"
grep -q 'Y2VsbGJveC1zdHJlYW0tb2s=' "$work_dir/events"
grep -q '"exit_code":0' "$work_dir/events"

printf 'cellbox-file-roundtrip\n' >"$work_dir/upload.txt"
curl -fsS -X PUT "$base/v1/sessions/$session_id/files?path=%2Ftmp%2Fcellbox-upload.txt" \
	-H "Authorization: Bearer $api_key" \
	-H 'Content-Type: application/octet-stream' \
	--data-binary "@$work_dir/upload.txt"
curl -fsS "$base/v1/sessions/$session_id/files/list?path=%2Ftmp" \
	-H "Authorization: Bearer $api_key" |
	jq -e --arg path '/tmp/cellbox-upload.txt' '.items[] | select(.path == $path)' >/dev/null
curl -fsS "$base/v1/sessions/$session_id/files?path=%2Ftmp%2Fcellbox-upload.txt" \
	-H "Authorization: Bearer $api_key" -o "$work_dir/download.txt"
cmp "$work_dir/upload.txt" "$work_dir/download.txt"
if [[ "${CELLBOX_PYTHON_SDK:-0}" == "1" ]]; then
	CELLBOX_URL="$base" CELLBOX_KEY="$api_key" PYTHONPATH="$repo_root/sdk/python/src" \
		python3 -m pytest -q "$repo_root/sdk/python/tests" -m integration
fi
if [[ "${CELLBOX_PLAYWRIGHT:-0}" == "1" ]]; then
	CELLBOX_URL="$base" CELLBOX_KEY="$api_key" npm run test:e2e --prefix "$repo_root/web"
fi
curl -fsS -X DELETE "$base/v1/sessions/$session_id" \
	-H "Authorization: Bearer $api_key" >/dev/null
session_id=
echo "Docker API walkthrough passed"
