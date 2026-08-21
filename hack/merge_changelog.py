#!/usr/bin/env python3
"""Merges a CHANGELOG.md freshly checked out from one branch (master or an LTS
release-* branch) into the already-published CHANGELOG.md, without ever copying
entries between branches by hand.

Every version present in source is kept; any version present in target but not
source is kept too. When the same version exists in both, source's copy wins,
since source is freshly checked out from the operator repo and target (vmdocs)
may hold a stale hand-edit. Versions are never removed, so deleting the source
branch later has no effect on what was already merged. If --master is passed,
source's "tip" section replaces target's, since only master owns the
"unreleased changes" section; a release-* branch's own tip (its not-yet-tagged
changes) is ignored.
"""
import argparse
import re
import sys

import compare_versions

HEADING_RE = re.compile(r"^## .*$", re.MULTILINE)
VERSION_RE = re.compile(r"^## \[(\S+)\]")


class Version:
    def __init__(self, heading, body):
        self.heading = heading
        self.body = body
        self.version = parse_version(heading)


def parse_version(heading):
    m = VERSION_RE.match(heading)
    if not m:
        return None
    try:
        return compare_versions.parse_version(m.group(1))
    except ValueError:
        return None


class ChangelogFile:
    def __init__(self, front_matter, tip, versions):
        self.front_matter = front_matter
        self.tip = tip
        self.versions = versions

    def render(self):
        parts = [self.front_matter.rstrip("\n"), "\n\n## tip\n\n", self.tip]
        for v in self.versions:
            parts.append(f"\n\n{v.heading}\n{v.body}")
        parts.append("\n")
        return "".join(parts)


def split_front_matter(data):
    if not data.startswith("---\n"):
        return "", data
    end = data.find("\n---\n", 4)
    if end == -1:
        raise ValueError("unterminated front matter")
    end += len("\n---\n")
    return data[:end], data[end:]


def parse(data):
    front_matter, rest = split_front_matter(data)

    matches = list(HEADING_RE.finditer(rest))
    if not matches:
        raise ValueError('no "## " headings found')

    tip = ""
    versions = []
    for i, m in enumerate(matches):
        start = m.end()
        end = matches[i + 1].start() if i + 1 < len(matches) else len(rest)
        heading = m.group().strip()
        body = rest[start:end].strip("\n")

        if heading == "## tip":
            tip = body
            continue
        versions.append(Version(heading, body))

    return ChangelogFile(front_matter, tip, versions)


def merge(target, source, source_is_master):
    tip = source.tip if source_is_master else target.tip
    by_heading = {v.heading: v for v in target.versions}
    by_heading.update((v.heading, v) for v in source.versions)
    versions = list(by_heading.values())
    versions.sort(key=lambda v: (v.version is not None, v.version or ()), reverse=True)
    return ChangelogFile(target.front_matter, tip, versions)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--target", required=True, help="already-published CHANGELOG.md to update in place"
    )
    parser.add_argument(
        "--source", required=True, help="CHANGELOG.md freshly checked out from one branch"
    )
    parser.add_argument(
        "--master", action="store_true", help="source was checked out from the master branch"
    )
    args = parser.parse_args()

    with open(args.target, encoding="utf-8") as f:
        target = parse(f.read())
    with open(args.source, encoding="utf-8") as f:
        source = parse(f.read())

    merged = merge(target, source, args.master)

    with open(args.target, "w", encoding="utf-8") as f:
        f.write(merged.render())

    return 0


if __name__ == "__main__":
    sys.exit(main())
