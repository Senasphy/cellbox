#!/usr/bin/env bash
set -euo pipefail

CELLBOX_BIN=${CELLBOX_BIN:-./cellbox}
DOCKER_CLI=${DOCKER_CLI:-docker}
CURL=${CURL:-curl}
JQ=${JQ:-jq}
DOCKER_SOCKET=${CELLBOX_DOCKER_SOCKET:-/var/run/docker.sock}
tmp_dir=$(mktemp -d)
listen_port=$((18000 + $$ % 1000))
listen_addr="127.0.0.1:${listen_port}"
service_pid=
session_id=
admin_key=
base_url="http://${listen_addr}"

cleanup() {
  if [[ -n "${session_id}" && -n "${admin_key}" ]]; then
    "${CURL}" -fsS -X DELETE "${base_url}/v1/sessions/${session_id}" \
      -H "Authorization: Bearer ${admin_key}" >/dev/null 2>&1 || true
  fi
  if [[ -n "${service_pid}" ]] && kill -0 "${service_pid}" 2>/dev/null; then
    kill -TERM "${service_pid}" 2>/dev/null || true
    wait "${service_pid}" 2>/dev/null || true
  fi
  if [[ -n "${session_id}" ]]; then
    mapfile -t orphan_ids < <("${DOCKER_CLI}" ps -aq --filter "label=io.cellbox.session=${session_id}")
    if [[ ${#orphan_ids[@]} -gt 0 ]]; then
      "${DOCKER_CLI}" container rm --force "${orphan_ids[@]}" >/dev/null 2>&1 || true
    fi
  fi
  python3 -c 'import shutil,sys; shutil.rmtree(sys.argv[1], ignore_errors=True)' "${tmp_dir}"
}
trap cleanup EXIT

cat >"${tmp_dir}/cellbox.yaml" <<EOF
listen: "${listen_addr}"
data_dir: "${tmp_dir}/data"
backend: docker
docker_socket: "${DOCKER_SOCKET}"
EOF

start_service() {
  "${CELLBOX_BIN}" serve --config "${tmp_dir}/cellbox.yaml" >"${tmp_dir}/service.log" 2>&1 &
  service_pid=$!
  for _ in $(seq 1 60); do
    if "${CURL}" -fsS "${base_url}/readyz" >/dev/null 2>&1; then
      return
    fi
    if ! kill -0 "${service_pid}" 2>/dev/null; then
      cat "${tmp_dir}/service.log" >&2
      return 1
    fi
    sleep 1
  done
  cat "${tmp_dir}/service.log" >&2
  return 1
}

start_service
admin_key=$(sed -n 's/^admin key (shown once): //p' "${tmp_dir}/service.log" | head -n 1)
if [[ -z "${admin_key}" ]]; then
  cat "${tmp_dir}/service.log" >&2
  echo "startup did not print the initial admin key" >&2
  exit 1
fi

created=$("${CURL}" -fsS -X POST "${base_url}/v1/sessions" \
  -H "Authorization: Bearer ${admin_key}" \
  -H 'Content-Type: application/json' \
  -d '{"image":"alpine:latest","network_policy":{"mode":"none"}}')
session_id=$(printf '%s' "${created}" | "${JQ}" -r .id)
if [[ -z "${session_id}" || "${session_id}" == "null" ]]; then
  echo "session creation returned no ID: ${created}" >&2
  exit 1
fi
"${CURL}" -fsS -X POST "${base_url}/v1/sessions/${session_id}/execs" \
  -H "Authorization: Bearer ${admin_key}" \
  -H 'Content-Type: application/json' \
  -d '{"argv":["sh","-lc","sleep 300"]}' >/dev/null
sleep 1

kill -KILL "${service_pid}"
wait "${service_pid}" 2>/dev/null || true
service_pid=

start_service
state=$("${CURL}" -fsS "${base_url}/v1/sessions/${session_id}" \
  -H "Authorization: Bearer ${admin_key}" | "${JQ}" -r .state)
if [[ "${state}" != "lost" ]]; then
  echo "session state after restart is ${state}; want lost" >&2
  exit 1
fi
orphans=$("${DOCKER_CLI}" ps -aq --filter "label=io.cellbox.session=${session_id}")
if [[ -n "${orphans}" ]]; then
  echo "orphan Docker containers remain after restart: ${orphans}" >&2
  exit 1
fi
echo "crash recovery walkthrough passed"
