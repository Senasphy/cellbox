"""Deterministic offline fix loop used by CI; no hosted model is contacted."""
from __future__ import annotations
import argparse
import os
import time
from cellbox import Client

def main() -> int:
    parser=argparse.ArgumentParser();parser.add_argument("--fake-model",action="store_true");args=parser.parse_args()
    if not args.fake_model: parser.error("this compact demo currently supports --fake-model")
    with Client(os.getenv("CELLBOX_URL","http://127.0.0.1:8080"),os.environ["CELLBOX_KEY"]) as client:
        with client.sessions.create("python:3.12-slim",memory_mb=512,network_policy={"mode":"none"}) as box:
            box.exec(["mkdir", "-p", "/work"])
            box.write_file("/work/test_app.py","assert add(2, 3) == 5\n")
            box.write_file("/work/app.py","def add(a, b):\n    return a - b\n")
            for attempt in range(1,4):
                result=box.exec(["python","-c","from app import add; exec(open('test_app.py').read())"],cwd="/work",timeout_s=30)
                print(f"attempt {attempt}: exit={result.exit_code} {result.stderr.strip()}")
                if result.exit_code==0: return 0
                if attempt==1: box.write_file("/work/app.py","def add(a, b):\n    return a + b\n")
            return 1
if __name__=="__main__": raise SystemExit(main())
