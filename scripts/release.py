#!/usr/bin/env python3
"""
Release automation script for Kestrix.
Maintains CHANGELOG.md as single source of truth for versioning,
cuts release tags, and pushes them to trigger CI/CD releases.
"""

import argparse
import datetime
import os
import re
import shutil
import subprocess
import sys


def run_command(cmd: list[str], check: bool = True) -> subprocess.CompletedProcess:
    """Run a command and return the result."""
    print(f"Running: {' '.join(cmd)}")
    try:
        result = subprocess.run(cmd, capture_output=True, text=True, check=check)
        if result.stdout:
            print(result.stdout.rstrip())
        if result.stderr:
            print(result.stderr.rstrip(), file=sys.stderr)
        return result
    except subprocess.CalledProcessError as e:
        print(f"Command failed with exit code {e.returncode}")
        if e.stdout:
            print(f"stdout: {e.stdout}")
        if e.stderr:
            print(f"stderr: {e.stderr}", file=sys.stderr)
        sys.exit(1)


def get_current_version(changelog_path: str = "CHANGELOG.md") -> str:
    """Get current latest version from CHANGELOG.md."""
    try:
        with open(changelog_path, "r", encoding="utf-8") as f:
            content = f.read()
        match = re.search(r"^##\s+\[v?([0-9]+\.[0-9]+\.[0-9]+[^\]]*)\]", content, re.MULTILINE)
        if match:
            return match.group(1).lstrip("v").strip()
        print(f"Error: No version heading found in {changelog_path}")
        sys.exit(1)
    except FileNotFoundError:
        print(f"Error: Could not find {changelog_path}")
        sys.exit(1)


def has_jj() -> bool:
    """Check if jj (Jujutsu) is installed and this is a jj repository."""
    return bool(shutil.which("jj") and os.path.isdir(".jj"))


def check_working_tree_clean():
    """Check if working tree is clean."""
    if has_jj():
        res = run_command(["jj", "diff"], check=False)
        if res.stdout.strip():
            print("Error: Jujutsu working copy has uncommitted modifications.")
            sys.exit(1)
    else:
        result = run_command(["git", "status", "--porcelain"], check=False)
        if result.stdout.strip():
            print("Error: Git working directory is not clean. Please commit or stash changes.")
            print(result.stdout)
            sys.exit(1)


def calculate_bump(current_version: str, bump_type: str) -> str:
    """Calculate next semver version based on bump_type (patch, minor, major)."""
    parts = current_version.split(".")
    if len(parts) != 3:
        print(f"Error: Cannot bump non-standard semver version: {current_version}")
        sys.exit(1)

    try:
        major, minor, patch = int(parts[0]), int(parts[1]), int(parts[2])
    except ValueError:
        print(f"Error: Cannot parse integers from version: {current_version}")
        sys.exit(1)

    if bump_type == "patch":
        patch += 1
    elif bump_type == "minor":
        minor += 1
        patch = 0
    elif bump_type == "major":
        major += 1
        minor = 0
        patch = 0
    else:
        print(f"Error: Unknown bump type '{bump_type}'. Choose 'patch', 'minor', or 'major'.")
        sys.exit(1)

    return f"{major}.{minor}.{patch}"


def update_changelog(new_version: str, changelog_path: str = "CHANGELOG.md") -> bool:
    """
    Ensure CHANGELOG.md has an entry for new_version.
    If new_version is not yet present, promotes [Unreleased] to [vnew_version] - YYYY-MM-DD.
    Returns True if file was updated, False if version section was already present.
    """
    clean_version = new_version.lstrip("v").strip()
    tag = f"v{clean_version}"
    with open(changelog_path, "r", encoding="utf-8") as f:
        content = f.read()

    # Check if section already exists
    if re.search(rf"^##\s+\[v?{re.escape(clean_version)}\]", content, re.MULTILINE):
        print(f"Changelog already contains section for version {tag}")
        return False

    today = datetime.date.today().strftime("%Y-%m-%d")
    unreleased_pattern = re.compile(r"^##\s+\[Unreleased\]\s*\n", re.MULTILINE)

    if not unreleased_pattern.search(content):
        print(f"Error: No '## [Unreleased]' section found in {changelog_path}")
        sys.exit(1)

    replacement = f"## [Unreleased]\n\n## [{tag}] - {today}\n"
    new_content = unreleased_pattern.sub(replacement, content, count=1)

    with open(changelog_path, "w", encoding="utf-8") as f:
        f.write(new_content)

    print(f"Updated {changelog_path}: cut release [{tag}] - {today}")
    return True


def commit_and_tag(version: str, push: bool = False, changelog_updated: bool = True):
    """Commit version changes and create git tag."""
    tag_name = f"v{version.lstrip('v')}"

    if has_jj():
        if changelog_updated:
            print("Describing Jujutsu commit...")
            run_command(["jj", "describe", "-m", f"chore: release {tag_name}"])
            run_command(["jj", "bookmark", "set", "main", "-r", "@"])
        print(f"Creating tag {tag_name}...")
        run_command(["git", "tag", "-a", tag_name, "-m", f"Release {tag_name}"])
        if push:
            print(f"Pushing Jujutsu bookmark main and tag {tag_name}...")
            run_command(["jj", "git", "push"])
            run_command(["git", "push", "origin", tag_name])
    else:
        if changelog_updated:
            print("Committing changelog updates...")
            run_command(["git", "add", "CHANGELOG.md"])
            run_command(["git", "commit", "-m", f"chore: release {tag_name}"])
        print(f"Creating git tag {tag_name}...")
        run_command(["git", "tag", "-a", tag_name, "-m", f"Release {tag_name}"])
        if push:
            print(f"Pushing commit and tag {tag_name} to remote...")
            run_command(["git", "push", "origin", "main"])
            run_command(["git", "push", "origin", tag_name])


def main():
    parser = argparse.ArgumentParser(description="Automate release process for Kestrix")
    parser.add_argument(
        "version",
        help="New version number (e.g., 0.3.1, v0.3.1, patch, minor, major)",
    )
    parser.add_argument(
        "--push",
        action="store_true",
        help="Push commit and tag to remote repository",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Show what would be done without making changes",
    )
    parser.add_argument(
        "--changelog-path",
        default="CHANGELOG.md",
        help="Path to CHANGELOG.md (default: CHANGELOG.md)",
    )

    args = parser.parse_args()

    if args.dry_run:
        print("🔍 DRY RUN MODE - No changes will be made")

    current_version = get_current_version(args.changelog_path)
    print(f"Current version: {current_version}")

    raw_version = args.version.strip()
    if raw_version in ["patch", "minor", "major"]:
        target_version = calculate_bump(current_version, raw_version)
    else:
        target_version = raw_version.lstrip("v")

    tag_name = f"v{target_version}"
    print(f"Target version:  {target_version} (tag: {tag_name})")

    if not args.dry_run:
        check_working_tree_clean()
        changelog_updated = update_changelog(target_version, args.changelog_path)
        commit_and_tag(target_version, push=args.push, changelog_updated=changelog_updated)
        print(f"\n✅ Release {tag_name} completed successfully!")
        if not args.push:
            print(f"💡 To push to remote, run: git push origin main && git push origin {tag_name}")
        print("💡 The CI/CD pipeline will verify the tag and publish images/binaries.")
    else:
        print(f"Would update {args.changelog_path} with version [{tag_name}]")
        print(f"Would create commit 'chore: release {tag_name}'")
        print(f"Would create git tag '{tag_name}'")
        if args.push:
            print(f"Would push commit and tag '{tag_name}' to remote")


if __name__ == "__main__":
    main()
