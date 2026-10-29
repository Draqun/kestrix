#!/usr/bin/env python3
"""
Verify that an MR/PR provides a Changie change fragment in .changes/unreleased/.
Used in CI on merge_request_event (GitLab) and pull_request (GitHub) to block
MRs/PRs that omit required changelog notes.

Exits 0 if valid unreleased fragment(s) are found or check is skipped.
Exits 1 if no unreleased fragment is found or fragment is invalid.
Exits 2 on fatal errors.
"""

import argparse
import os
import re
import subprocess
import sys


VALID_KINDS = {"added", "changed", "fixed", "deprecated", "removed", "security"}


def is_check_skipped() -> bool:
    """Check environment variables and recent commit messages for skip directives."""
    # Check environment variables
    for env_var in ("SKIP_CHANGELOG_CHECK", "SKIP_VERSION_BUMP_CHECK"):
        val = os.environ.get(env_var, "").strip().lower()
        if val in ("true", "1", "yes"):
            print(f"Changelog check skipped via environment variable: {env_var}={val}")
            return True

    # Check commit messages
    try:
        # Check last commit message
        res = subprocess.run(
            ["git", "log", "-1", "--pretty=%B"],
            capture_output=True,
            text=True,
            check=False,
        )
        if res.returncode == 0:
            msg = res.stdout.lower()
            if "[skip changelog]" in msg or "[skip version-check]" in msg:
                print("Changelog check skipped via commit message flag.")
                return True
    except Exception:
        pass

    return False


def get_changed_fragments(target_ref: str, unreleased_dir: str) -> set[str]:
    """Find added or modified fragment files compared to target_ref and working tree."""
    fragments: set[str] = set()

    # 1. Committed additions/modifications between target_ref and HEAD (CI & pushed branches)
    try:
        cmd = ["git", "diff", "--name-only", "--diff-filter=d", f"{target_ref}...HEAD", "--", unreleased_dir]
        res = subprocess.run(cmd, capture_output=True, text=True, check=False)
        if res.returncode == 0:
            for line in res.stdout.splitlines():
                path = line.strip()
                if path and _is_fragment_file(path):
                    fragments.add(path)
    except Exception as e:
        print(f"Notice: git diff {target_ref}...HEAD failed: {e}")

    # 2. Local working tree diff vs index/HEAD (for local development, git or jj)
    try:
        cmd = ["git", "diff", "--name-only", "--diff-filter=d", "--", unreleased_dir]
        res = subprocess.run(cmd, capture_output=True, text=True, check=False)
        if res.returncode == 0:
            for line in res.stdout.splitlines():
                path = line.strip()
                if path and _is_fragment_file(path):
                    fragments.add(path)
    except Exception:
        pass

    # 3. Staged additions (for local pre-commit checks)
    try:
        cmd = ["git", "diff", "--name-only", "--cached", "--diff-filter=d", "--", unreleased_dir]
        res = subprocess.run(cmd, capture_output=True, text=True, check=False)
        if res.returncode == 0:
            for line in res.stdout.splitlines():
                path = line.strip()
                if path and _is_fragment_file(path):
                    fragments.add(path)
    except Exception:
        pass

    # 4. Status porcelain (catches untracked and jj working-copy files)
    try:
        cmd = ["git", "status", "--porcelain", "--", unreleased_dir]
        res = subprocess.run(cmd, capture_output=True, text=True, check=False)
        if res.returncode == 0:
            for line in res.stdout.splitlines():
                if len(line) > 3 and line[0] != "D" and line[1] != "D":
                    path = line[3:].strip()
                    if path and _is_fragment_file(path):
                        fragments.add(path)
    except Exception:
        pass

    return fragments


def _is_fragment_file(path: str) -> bool:
    """Return True if path looks like a changelog fragment (YAML file, not .gitkeep)."""
    base = os.path.basename(path)
    if base.startswith(".") or base in ("header.tpl.md", "footer.tpl.md", "README.md"):
        return False
    return base.endswith(".yaml") or base.endswith(".yml")


def validate_fragment(path: str) -> tuple[bool, str]:
    """Verify that a fragment has a valid kind and non-empty body."""
    if not os.path.isfile(path):
        return False, f"File does not exist: {path}"

    try:
        with open(path, "r", encoding="utf-8") as f:
            content = f.read()
    except Exception as exc:
        return False, f"Cannot read file {path}: {exc}"

    # Extract kind
    kind_match = re.search(r"^kind:\s*([A-Za-z]+)", content, re.MULTILINE)
    if not kind_match:
        return False, f"{path}: Missing 'kind:' attribute"

    kind = kind_match.group(1).strip().lower()
    if kind not in VALID_KINDS:
        valid_list = ", ".join(sorted(VALID_KINDS))
        return False, f"{path}: Invalid kind '{kind}'. Valid kinds: {valid_list}"

    # Extract body
    body_match = re.search(r"^body:\s*(.+)", content, re.MULTILINE)
    if not body_match or not body_match.group(1).strip():
        return False, f"{path}: Missing or empty 'body:' attribute"

    return True, ""


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--target-ref",
        default="origin/main",
        help="Git ref of the target branch (default: origin/main)",
    )
    parser.add_argument(
        "--unreleased-dir",
        default=".changes/unreleased",
        help="Path to unreleased fragments directory (default: .changes/unreleased)",
    )
    args = parser.parse_args()

    if is_check_skipped():
        return 0

    if not os.path.isdir(args.unreleased_dir):
        print(f"Error: unreleased directory '{args.unreleased_dir}' not found.")
        return 2

    # Verify if target ref exists; if not, try to fetch or check without origin/
    target_ref = args.target_ref
    check_ref = subprocess.run(
        ["git", "rev-parse", "--verify", target_ref],
        capture_output=True,
        text=True,
        check=False,
    )
    if check_ref.returncode != 0:
        # Try stripping origin/ if local branch exists
        if target_ref.startswith("origin/"):
            local_alt = target_ref.removeprefix("origin/")
            alt_res = subprocess.run(
                ["git", "rev-parse", "--verify", local_alt],
                capture_output=True,
                text=True,
                check=False,
            )
            if alt_res.returncode == 0:
                target_ref = local_alt
            else:
                print(f"Warning: target ref '{target_ref}' and '{local_alt}' not found in git.")
        else:
            print(f"Warning: target ref '{target_ref}' not found in git.")

    fragments = get_changed_fragments(target_ref, args.unreleased_dir)

    # Filter only files that currently exist on disk
    existing_fragments = [f for f in fragments if os.path.isfile(f)]

    if not existing_fragments:
        print(
            "\n"
            "======================================================================\n"
            "ERROR: No changelog fragment found in .changes/unreleased/!\n"
            "======================================================================\n"
            "Every MR/PR must provide a changelog fragment describing its changes.\n\n"
            "To create one, run:\n"
            "    make changelog-new KIND=Added BODY=\"Description of your change\"\n"
            "or:\n"
            "    changie new\n\n"
            "Valid kinds: Added, Changed, Fixed, Deprecated, Removed, Security\n\n"
            "If this change is purely internal and requires no release notes,\n"
            "add '[skip changelog]' to your commit message.\n"
            "======================================================================\n"
        )
        return 1

    # Validate each fragment
    all_valid = True
    for frag in existing_fragments:
        valid, err = validate_fragment(frag)
        if not valid:
            print(f"Error in changelog fragment: {err}")
            all_valid = False

    if not all_valid:
        return 1

    print(f"Changelog fragment verification PASSED. Found {len(existing_fragments)} fragment(s):")
    for frag in sorted(existing_fragments):
        print(f"  - {frag}")

    return 0


if __name__ == "__main__":
    sys.exit(main())
