#!/usr/bin/env python3
"""
Verify that CHANGELOG.md version has been bumped vs the target branch.
Used in CI on merge_request_event to block MRs that forget to bump the version.
Exits 0 if version differs, 1 if equal, 2 on read errors.
"""

import argparse
import re
import subprocess
import sys


def _read_version_from_text(text: str) -> str:
    """Parse version from CHANGELOG.md text content."""
    match = re.search(r"^##\s+\[v?([0-9]+\.[0-9]+\.[0-9]+[^\]]*)\]", text, re.MULTILINE)
    if match:
        return match.group(1).lstrip("v").strip()
    raise ValueError("No version heading (## [X.Y.Z] or ## [vX.Y.Z]) found in text")


def get_current_version(changelog_path: str) -> str:
    """Load version from local CHANGELOG.md file."""
    with open(changelog_path, encoding="utf-8") as f:
        return _read_version_from_text(f.read())


def get_target_version(ref: str, changelog_path: str) -> str:
    """Load version from CHANGELOG.md as it exists on the given git ref."""
    result = subprocess.run(
        ["git", "show", f"{ref}:{changelog_path}"],
        capture_output=True,
        text=True,
        check=True,
    )
    return _read_version_from_text(result.stdout)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--target-ref",
        required=True,
        help="Git ref of the target branch (e.g. origin/main)",
    )
    parser.add_argument(
        "--changelog-path",
        default="CHANGELOG.md",
        help="Path to CHANGELOG.md (default: CHANGELOG.md)",
    )
    args = parser.parse_args()

    try:
        current = get_current_version(args.changelog_path)
    except Exception as exc:
        print(f"Error: cannot read current version: {exc}")
        return 2

    try:
        target = get_target_version(args.target_ref, args.changelog_path)
    except subprocess.CalledProcessError as exc:
        print(f"Warning: git show {args.target_ref} failed (possibly new branch or target without changelog): {exc.stderr}")
        return 0
    except Exception as exc:
        print(f"Warning: cannot parse version from {args.target_ref}: {exc}")
        return 0

    print(f"Current version (MR):             {current}")
    print(f"Target version ({args.target_ref}): {target}")

    if current == target:
        print(
            "\nVersion has NOT been bumped.\n"
            "Update CHANGELOG.md with a new release section before merging — "
            "otherwise the release tag step will collide with an existing tag.",
        )
        return 1

    print("\nVersion bumped — OK.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
