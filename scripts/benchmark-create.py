#!/usr/bin/env python3
"""Compare cold session creation with claimed warm-pool session creation."""

import argparse
import json
import os
import platform
import re
import statistics
import time
import urllib.error
import urllib.request


def request(base_url, key, method, path, body=None):
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(
        base_url.rstrip("/") + path,
        data=data,
        method=method,
        headers={"Authorization": f"Bearer {key}", "Content-Type": "application/json"},
    )
    try:
        with urllib.request.urlopen(req, timeout=30) as response:
            payload = response.read()
            return response.status, json.loads(payload) if payload else None
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", errors="replace")[:500]
        raise RuntimeError(f"{method} {path} returned HTTP {exc.code}: {detail}") from exc


def metric(base_url, key, name, labels=""):
    req = urllib.request.Request(
        base_url.rstrip("/") + "/metrics",
        headers={"Authorization": f"Bearer {key}"},
    )
    with urllib.request.urlopen(req, timeout=30) as response:
        text = response.read().decode("utf-8", errors="replace")
    prefix = name + ("{" + labels + "}" if labels else "")
    for line in text.splitlines():
        if line.startswith(prefix + " "):
            return float(line.rsplit(" ", 1)[1])
    return 0.0


def create_sample(base_url, key, image):
    started = time.perf_counter()
    _, session = request(base_url, key, "POST", "/v1/sessions", {"image": image, "network_policy": {"mode": "none"}})
    elapsed_ms = (time.perf_counter() - started) * 1000
    if not session or not session.get("id") or session.get("state") != "ready":
        raise RuntimeError(f"session creation did not return a ready session: {session!r}")
    return elapsed_ms, session["id"]


def destroy_sample(base_url, key, session_id):
    request(base_url, key, "DELETE", f"/v1/sessions/{session_id}")


def wait_for_warm(base_url, key, image, timeout_s):
    labels = f"image={json.dumps(image)}"
    deadline = time.monotonic() + timeout_s
    while time.monotonic() < deadline:
        if metric(base_url, key, "cellbox_pool_ready", labels) > 0:
            return
        time.sleep(0.2)
    raise RuntimeError(f"warm pool did not reach ready state for {image!r} within {timeout_s:g}s")


def validate_service(base_url, key, should_have_pool):
    _, system = request(base_url, key, "GET", "/v1/system")
    has_pool = bool(system.get("capabilities", {}).get("warm_pool"))
    if has_pool != should_have_pool:
        expected = "enabled" if should_have_pool else "disabled"
        raise RuntimeError(f"{base_url} warm pool must be {expected}")


def summarize(samples):
    ordered = sorted(samples)
    p95_index = max(0, min(len(ordered) - 1, int(0.95 * len(ordered) + 0.999999) - 1))
    return {"samples": len(samples), "p50_ms": statistics.median(samples), "p95_ms": ordered[p95_index], "values_ms": samples}


def host_info():
    memory_mb = None
    memory_limit_mb = None
    cpu_quota_millicores = None
    try:
        with open("/proc/meminfo", encoding="ascii") as source:
            for line in source:
                if line.startswith("MemTotal:"):
                    memory_mb = int(line.split()[1]) // 1024
                    break
    except OSError:
        pass
    try:
        with open("/sys/fs/cgroup/memory.max", encoding="ascii") as source:
            value = source.read().strip()
            if value != "max":
                memory_limit_mb = int(value) // (1024 * 1024)
    except (OSError, ValueError):
        pass
    try:
        with open("/sys/fs/cgroup/cpu.max", encoding="ascii") as source:
            quota, period = source.read().split()
            if quota != "max":
                cpu_quota_millicores = int(quota) * 1000 // int(period)
    except (OSError, ValueError, ZeroDivisionError):
        pass
    return {
        "platform": platform.platform(),
        "kernel": platform.release(),
        "cpu_count": os.cpu_count(),
        "cpu_quota_millicores": cpu_quota_millicores,
        "memory_mb": memory_mb,
        "memory_limit_mb": memory_limit_mb,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--cold-url", default=os.getenv("CELLBOX_COLD_URL", "http://127.0.0.1:8081"))
    parser.add_argument("--warm-url", default=os.getenv("CELLBOX_WARM_URL", "http://127.0.0.1:8080"))
    parser.add_argument("--image", default="alpine:latest")
    parser.add_argument("--samples", type=int, default=20)
    parser.add_argument("--warm-timeout", type=float, default=300)
    args = parser.parse_args()
    cold_key = os.environ.get("CELLBOX_COLD_KEY", "")
    warm_key = os.environ.get("CELLBOX_WARM_KEY", "")
    if not cold_key or not warm_key:
        parser.error("set CELLBOX_COLD_KEY and CELLBOX_WARM_KEY")
    if args.samples < 1 or args.warm_timeout <= 0:
        parser.error("--samples and --warm-timeout must be positive")

    validate_service(args.cold_url, cold_key, False)
    validate_service(args.warm_url, warm_key, True)
    cold, warm = [], []
    for _ in range(args.samples):
        duration, session_id = create_sample(args.cold_url, cold_key, args.image)
        cold.append(duration)
        destroy_sample(args.cold_url, cold_key, session_id)

    # This also seeds refill when --image is not among the service's curated images.
    _, priming_session_id = create_sample(args.warm_url, warm_key, args.image)
    destroy_sample(args.warm_url, warm_key, priming_session_id)
    wait_for_warm(args.warm_url, warm_key, args.image, args.warm_timeout)

    warm_labels = 'source="warm"'
    for _ in range(args.samples):
        wait_for_warm(args.warm_url, warm_key, args.image, args.warm_timeout)
        before = metric(args.warm_url, warm_key, "cellbox_session_create_seconds_count", warm_labels)
        duration, session_id = create_sample(args.warm_url, warm_key, args.image)
        after = metric(args.warm_url, warm_key, "cellbox_session_create_seconds_count", warm_labels)
        if after <= before:
            destroy_sample(args.warm_url, warm_key, session_id)
            raise RuntimeError("warm readiness was present, but session creation used the cold path")
        warm.append(duration)
        destroy_sample(args.warm_url, warm_key, session_id)

    print(json.dumps({"image": args.image, "host": host_info(), "cold": summarize(cold), "warm": summarize(warm)}, indent=2))


if __name__ == "__main__":
    main()
