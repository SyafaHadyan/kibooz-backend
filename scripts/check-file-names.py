#!/usr/bin/env python3
"""Checks the names of the files that git tracks against the naming of the repository, and exits with 1 and lists every
file that breaks a rule. It reads only the list of names and never opens a file, and it needs nothing but the standard library.

The rules are the ones the repository already follows, see docs/ci-cd.md. The first rule that applies to a path decides.
"""

import re
import subprocess
import sys

KEBAB = r"[a-z0-9]+(?:-[a-z0-9]+)*"

# (description, applies to the path, the name has to match) in the order they are tried
RULES = [
    (
        "a migration is a six digit number, snake case words and .up.sql or .down.sql",
        re.compile(r"^internal/infra/db/migrations/[^/]+$"),
        re.compile(r"^[0-9]{6}_[a-z0-9]+(?:_[a-z0-9]+)*\.(?:up|down)\.sql$"),
    ),
    (
        "a Go file is lowercase words with no separator, and only _test may follow",
        re.compile(r"\.go$"),
        re.compile(r"^[a-z0-9]+(?:_test)?\.go$"),
    ),
    (
        "a decision record is four digits, kebab case words and .md",
        re.compile(r"^docs/adr/[^/]+$"),
        re.compile(r"^(?:[0-9]{4}-" + KEBAB + r"|README)\.md$"),
    ),
    (
        "a workflow is kebab case and ends with .yaml",
        re.compile(r"^\.github/workflows/[^/]+$"),
        re.compile(r"^" + KEBAB + r"\.yaml$"),
    ),
    (
        "a script is kebab case with .sh, .py or .js",
        re.compile(r"\.(?:sh|py|js)$"),
        re.compile(r"^" + KEBAB + r"\.(?:sh|py|js)$"),
    ),
]

# names that other tools fix, so they are allowed as they are. Each entry is a pattern for the whole path.
ALLOWED = [
    # the community files that GitHub and the license tools look for by name
    r"^(?:[A-Z][A-Z_]*\.md|LICENSE)$",
    r"^\.github/(?:CODEOWNERS|PULL_REQUEST_TEMPLATE\.md)$",
    r"^\.github/ISSUE_TEMPLATE/[a-z0-9_]+\.yml$",
    # Vale names a style after its folder
    r"^\.github/vale/styles/Kibooz/[A-Za-z]+\.yml$",
    # Cloudflare Pages reads this file by name
    r"^docs/_headers$",
    # Docker looks for this name
    r"^(?:.+/)?Dockerfile$",
]

# any other name is lowercase words joined by hyphens, with dots for the extension and for a variant such as .example.yml,
# and a name that starts with a dot is a tool setting
SEGMENT = re.compile(r"^(?:\.?[a-z0-9]+(?:[.-][a-z0-9]+)*)$")
DOT_SEGMENT = re.compile(r"^\.[a-z0-9]+(?:[._-][a-z0-9]+)*$")


def problems(path):
    if any(re.search(pattern, path) for pattern in ALLOWED):
        return []

    name = path.rsplit("/", 1)[-1]

    for description, applies, accepts in RULES:
        if applies.search(path):
            return [] if accepts.match(name) else [description]

    for segment in path.split("/"):
        if not (SEGMENT.match(segment) or DOT_SEGMENT.match(segment)):
            return [f"'{segment}' is not lowercase words joined by hyphens"]

    return []


def main():
    listed = subprocess.run(["git", "ls-files", "-z"], capture_output=True, check=True).stdout.decode()
    paths = [p for p in listed.split("\0") if p]

    found = []

    for path in paths:
        for reason in problems(path):
            found.append(f"{path}\n    {reason}")

    # two names that differ only by case cannot live together on a case insensitive disk
    seen = {}

    for path in paths:
        other = seen.setdefault(path.lower(), path)
        if other != path:
            found.append(f"{path}\n    differs from {other} only by case")

    if found:
        print("These files do not follow the naming of the repository, see docs/ci-cd.md.\n")
        print("\n".join(found))
        print(f"\n{len(found)} problem(s) in {len(paths)} files")
        return 1

    print(f"{len(paths)} files follow the naming of the repository")
    return 0


if __name__ == "__main__":
    sys.exit(main())
