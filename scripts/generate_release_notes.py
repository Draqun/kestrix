#!/usr/bin/env python3
"""
Generate release notes for GitLab and GitHub releases.
Extracts notes from CHANGELOG.md or generates them from git commit history.
"""

import argparse
import os
import re
import subprocess
import sys


def get_latest_version_from_changelog(changelog_path: str = "CHANGELOG.md") -> str:
    """Get the latest version from CHANGELOG.md."""
    try:
        with open(changelog_path, "r", encoding="utf-8") as f:
            content = f.read()
        match = re.search(r"^##\s+\[v?([0-9]+\.[0-9]+\.[0-9]+[^\]]*)\]", content, re.MULTILINE)
        if match:
            return match.group(1).lstrip("v").strip()
    except FileNotFoundError:
        pass
    return "unknown"


def extract_changelog_section(version: str, changelog_path: str = "CHANGELOG.md") -> str | None:
    """Extract release notes for a specific version from CHANGELOG.md."""
    clean_version = version.lstrip("v").strip()
    try:
        with open(changelog_path, "r", encoding="utf-8") as f:
            lines = f.readlines()
    except FileNotFoundError:
        return None

    in_target_section = False
    section_lines = []

    header_pattern = re.compile(r"^##\s+\[v?([^\]]+)\]")

    for line in lines:
        match = header_pattern.match(line)
        if match:
            section_ver = match.group(1).lstrip("v").strip()
            if in_target_section:
                # Reached next section
                break
            if section_ver == clean_version:
                in_target_section = True
                continue
        elif in_target_section:
            section_lines.append(line)

    if in_target_section:
        content = "".join(section_lines).strip()
        if content:
            return content

    return None


def run_git_command(cmd: list[str]) -> str:
    """Run a git command and return stdout."""
    try:
        result = subprocess.run(["git"] + cmd, capture_output=True, text=True, check=True)
        return result.stdout.strip()
    except subprocess.CalledProcessError:
        return ""


def get_last_tag() -> str:
    """Get the last git tag."""
    return run_git_command(["describe", "--tags", "--abbrev=0", "HEAD^"])


def get_commits_since_tag(tag: str) -> list[str]:
    """Get non-merge commits since the last tag."""
    if not tag:
        commits = run_git_command(["log", "--oneline", "--no-merges"])
    else:
        commits = run_git_command(["log", f"{tag}..HEAD", "--oneline", "--no-merges"])
    return [line for line in commits.split("\n") if line.strip()]


def categorize_commits(commits: list[str]) -> dict[str, list[str]]:
    """Categorize commits by conventional commit prefix."""
    categories: dict[str, list[str]] = {
        "Features": [],
        "Bug Fixes": [],
        "Documentation": [],
        "Refactoring": [],
        "Tests": [],
        "CI/CD": [],
        "Other": [],
    }

    for commit in commits:
        commit_lower = commit.lower()
        if any(keyword in commit_lower for keyword in ["feat:", "feature:", "add:", "implement:"]):
            categories["Features"].append(commit)
        elif any(keyword in commit_lower for keyword in ["fix:", "bug:", "hotfix:"]):
            categories["Bug Fixes"].append(commit)
        elif any(keyword in commit_lower for keyword in ["docs:", "doc:", "documentation:"]):
            categories["Documentation"].append(commit)
        elif any(keyword in commit_lower for keyword in ["refactor:", "refact:", "cleanup:", "clean:"]):
            categories["Refactoring"].append(commit)
        elif any(keyword in commit_lower for keyword in ["test:", "tests:"]):
            categories["Tests"].append(commit)
        elif any(keyword in commit_lower for keyword in ["ci:", "cd:", "pipeline:", "release:"]):
            categories["CI/CD"].append(commit)
        else:
            categories["Other"].append(commit)

    return categories


def generate_notes_from_commits(version: str, categories: dict[str, list[str]]) -> str:
    """Generate notes from git commits."""
    notes = [f"## Release {version}\n"]
    for category, commits in categories.items():
        if commits:
            notes.append(f"### {category}\n")
            for commit in commits:
                parts = commit.split(" ", 1)
                if len(parts) == 2:
                    commit_hash, message = parts
                    notes.append(f"- {message} (`{commit_hash}`)")
                else:
                    notes.append(f"- {commit}")
            notes.append("")
    return "\n".join(notes).strip()


def main():
    parser = argparse.ArgumentParser(description="Generate release notes for releases")
    parser.add_argument(
        "--version",
        help="Version for the release (default: latest version from CHANGELOG.md)",
    )
    parser.add_argument(
        "--changelog-path",
        default="CHANGELOG.md",
        help="Path to CHANGELOG.md (default: CHANGELOG.md)",
    )
    parser.add_argument(
        "--output",
        help="Output file for release notes (default: stdout)",
    )

    args = parser.parse_args()

    version = args.version or get_latest_version_from_changelog(args.changelog_path)
    clean_version = version.lstrip("v").strip()

    # First attempt: extract from CHANGELOG.md
    notes = extract_changelog_section(clean_version, args.changelog_path)

    # Second attempt: fallback to git commits
    if not notes:
        last_tag = get_last_tag()
        commits = get_commits_since_tag(last_tag)
        if commits:
            categories = categorize_commits(commits)
            notes = generate_notes_from_commits(f"v{clean_version}", categories)
        else:
            notes = f"Release v{clean_version}"

    formatted_output = f"## Release v{clean_version}\n\n{notes}" if not notes.startswith("## ") else notes

    if args.output:
        with open(args.output, "w", encoding="utf-8") as f:
            f.write(formatted_output + "\n")
        print(f"Release notes written to {args.output}")
    else:
        print(formatted_output)


if __name__ == "__main__":
    main()
