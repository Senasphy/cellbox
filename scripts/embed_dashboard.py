#!/usr/bin/env python3
"""Copy the Next.js static export into the Go dashboard embed directory."""

from pathlib import Path
import shutil


ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "web" / "out"
DESTINATION = ROOT / "internal" / "dashboard" / "out"


def main() -> None:
    if not (SOURCE / "index.html").is_file():
        raise SystemExit("web/out/index.html is missing; build the dashboard before embedding")
    shutil.rmtree(DESTINATION, ignore_errors=True)
    shutil.copytree(SOURCE, DESTINATION)


if __name__ == "__main__":
    main()
