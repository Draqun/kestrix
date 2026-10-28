#!/usr/bin/env python3
"""
Script to verify that the version in CHANGELOG.md matches the Git tag.
This ensures that CHANGELOG.md is the single source of truth for versioning.
"""

import argparse
import re
import subprocess
import sys


def get_version_from_changelog(changelog_path: str = "CHANGELOG.md") -> str:
    """Load latest released version from CHANGELOG.md file."""
    try:
        with open(changelog_path, "r", encoding="utf-8") as f:
            content = f.read()
        match = re.search(r"^##\s+\[v?([0-9]+\.[0-9]+\.[0-9]+[^\]]*)\]", content, re.MULTILINE)
        if match:
            return match.group(1).strip()
        print(f"Error: No version section (## [X.Y.Z] or ## [vX.Y.Z]) found in {changelog_path}")
        sys.exit(1)
    except FileNotFoundError:
        print(f"Error: Could not find {changelog_path}")
        sys.exit(1)


def get_git_tag() -> str | None:
    """Get the current Git tag if we're on a tagged commit."""
    try:
        result = subprocess.run(
            ["git", "describe", "--exact-match", "--tags", "HEAD"],
            capture_output=True,
            text=True,
            check=False,
        )
        if result.returncode == 0:
            return result.stdout.strip()
        return None
    except subprocess.SubprocessError as e:
        print(f"Error: Could not get Git tag: {e}")
        return None


def normalize_version(version: str) -> str:
    """Normalize version string by removing 'v' prefix if present."""
    return version.lstrip("v").strip()


def main():
    """Main function to verify version consistency."""
    parser = argparse.ArgumentParser(description="Verify that CHANGELOG.md version matches Git tag")
    parser.add_argument(
        "--changelog-path",
        type=str,
        default="CHANGELOG.md",
        help="Path to CHANGELOG.md file (default: CHANGELOG.md)",
    )
    parser.add_argument(
        "--tag",
        type=str,
        help="Git tag to compare against (if not provided, uses current Git tag)",
    )
    parser.add_argument(
        "--strict",
        action="store_true",
        help="Exit with error if not on a tagged commit",
    )

    args = parser.parse_args()

    changelog_version = get_version_from_changelog(args.changelog_path)
    print(f"Version in CHANGELOG.md: {changelog_version}")

    if args.tag:
        git_tag = args.tag
        print(f"Provided Git tag: {git_tag}")
    else:
        git_tag = get_git_tag()
        if git_tag:
            print(f"Current Git tag: {git_tag}")
        else:
            print("Not on a tagged commit")
            if args.strict:
                print("Error: --strict mode requires being on a tagged commit")
                sys.exit(1)
            else:
                print("Skipping version verification (not on a tagged commit)")
                sys.exit(0)

    normalized_changelog = normalize_version(changelog_version)
    normalized_git = normalize_version(git_tag)

    print(f"Normalized CHANGELOG.md version: {normalized_changelog}")
    print(f"Normalized Git tag:             {normalized_git}")

    if normalized_changelog == normalized_git:
        print("✅ Version verification passed: CHANGELOG.md version matches Git tag")
        sys.exit(0)
    else:
        print("❌ Version mismatch!")
        print(f"   CHANGELOG.md version: {normalized_changelog}")
        print(f"   Git tag:              {normalized_git}")
        print("   Please update CHANGELOG.md version to match the Git tag")
        sys.exit(1)


if __name__ == "__main__":
    main()
