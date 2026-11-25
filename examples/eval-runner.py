"""Small deterministic eval runner. Tasks fork from one shared filesystem snapshot."""
import argparse, json, os
from concurrent.futures import ThreadPoolExecutor
from cellbox import Client

def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--fake-model", action="store_true", help="run the deterministic offline task set")
    args = parser.parse_args()
    tasks = [
        ("print(1 + 1)", "2"),
        ("print('cellbox')", "cellbox"),
        ("print(sum(range(5)))", "10"),
    ]

    url = os.getenv("CELLBOX_URL", "http://127.0.0.1:8080")
    key = os.environ["CELLBOX_KEY"]

    def run(index, item, base_id, snapshot_id):
        task, expected = item
        try:
            with Client(url, key) as client:
                parent = client.sessions.get(base_id)
                with parent.fork(snapshot_id) as session:
                    baseline = session.exec(["cat", "/tmp/eval-base.txt"])
                    if baseline.stdout.strip() != "shared eval base":
                        return {"task": task, "passed": False, "error": "fork did not contain the base snapshot file"}
                    late_parent_change = session.exec(["test", "!", "-e", "/tmp/eval-parent-after-snapshot.txt"])
                    if late_parent_change.exit_code != 0:
                        return {"task": task, "passed": False, "error": "fork included a parent change made after the snapshot"}
                    marker = f"/tmp/eval-child-{index}.txt"
                    session.write_file(marker, f"child {index}\n")
                    result = session.exec(["python", "-c", task])
                    output = result.stdout.strip()
                    return {
                        "task": task,
                        "output": output,
                        "exit_code": result.exit_code,
                        "duration_ms": result.duration_ms,
                        "state": result.state,
                        "marker": marker,
                        "passed": result.state == "exited" and result.exit_code == 0 and output == expected,
                    }
        except Exception as exc:
            return {"task": task, "passed": False, "error": f"{type(exc).__name__}: {exc}"}

    with Client(url, key) as client:
        with client.sessions.create("python:3.12-slim", memory_mb=512, network_policy={"mode": "none"}) as base:
            base.write_file("/tmp/eval-base.txt", "shared eval base\n")
            snapshot = base.snapshot(labels={"purpose": "eval-base"})
            base.write_file("/tmp/eval-parent-after-snapshot.txt", "must not appear in forks\n")
            isolation_error = ""
            with base.fork(snapshot["id"]) as first:
                if first.exec(["test", "!", "-e", "/tmp/eval-parent-after-snapshot.txt"]).exit_code != 0:
                    isolation_error = "first fork included a post-snapshot parent change"
                first.write_file("/tmp/eval-sibling-probe.txt", "first fork only\n")
            with base.fork(snapshot["id"]) as second:
                if second.exec(["test", "!", "-e", "/tmp/eval-sibling-probe.txt"]).exit_code != 0:
                    isolation_error = "sibling fork included another fork's write"
            if isolation_error:
                print(json.dumps({"results": [], "passed": 0, "failed": 1, "total": 1, "error": isolation_error}, indent=2))
                return 1
            with ThreadPoolExecutor(max_workers=len(tasks)) as pool:
                results = list(pool.map(lambda pair: run(pair[0], pair[1], base.id, snapshot["id"]), enumerate(tasks)))
            for result in results:
                marker = result.get("marker")
                if marker and base.exec(["test", "!", "-e", marker]).exit_code != 0:
                    result["passed"] = False
                    result["error"] = "child fork write leaked into the parent"
    passed = sum(result["passed"] for result in results)
    print(json.dumps({"results": results, "passed": passed, "failed": len(results) - passed, "total": len(results)}, indent=2))
    return 0 if passed == len(results) else 1


if __name__ == "__main__":
    raise SystemExit(main())
