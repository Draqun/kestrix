#!/usr/bin/env python3
"""List OpenSpec requirement headings and their status declared in spec text."""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

REQUIREMENT = re.compile(r"^###\s+(?:Requirement:\s*|REQ-\d+:\s*)(.+?)\s*$", re.MULTILINE)
STATUS = re.compile(r"\s*\((implemented|pending|partial|not implemented)\)\s*$", re.I)


def status_for(title: str) -> tuple[str, str]:
    match = STATUS.search(title)
    if not match:
        return title, "unmarked"
    return title[: match.start()].rstrip(), match.group(1).lower()


def requirements(path: Path) -> list[tuple[str, str]]:
    return [status_for(match.group(1)) for match in REQUIREMENT.finditer(path.read_text())]


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", type=Path, default=Path.cwd())
    parser.add_argument("--all", action="store_true", help="inventory every main spec")
    parser.add_argument("--spec", help="spec name, e.g. dag-engine")
    args = parser.parse_args()
    specs_dir = args.root / "openspec" / "specs"
    if not specs_dir.is_dir():
        print(f"OpenSpec specs directory not found: {specs_dir}", file=sys.stderr)
        return 2
    if bool(args.all) == bool(args.spec):
        parser.error("pass exactly one of --all or --spec")
    paths = sorted(specs_dir.glob("*/spec.md")) if args.all else [specs_dir / args.spec / "spec.md"]
    if not paths or not paths[0].is_file():
        print(f"Spec not found: {args.spec}", file=sys.stderr)
        return 2
    totals: dict[str, int] = {}
    for path in paths:
        spec = path.parent.name
        items = requirements(path)
        counts: dict[str, int] = {}
        for _, status in items:
            counts[status] = counts.get(status, 0) + 1
            totals[status] = totals.get(status, 0) + 1
        print(f"{spec}: requirements {len(items)}" + "".join(f" · {key} {value}" for key, value in sorted(counts.items())))
        for title, status in items:
            print(f"  - [{status}] {title}")
    print("TOTAL: requirements " + str(sum(totals.values())) + "".join(f" · {key} {value}" for key, value in sorted(totals.items())))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
