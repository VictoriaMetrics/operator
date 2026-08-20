#!/usr/bin/env python3
import sys


def parse_version(version):
    """Parses a version string into a tuple of integers."""
    return tuple(
        int(part) for part in version.removeprefix("v").split("-", 1)[0].split(".")
    )


def main():
    if len(sys.argv) != 3:
        print(
            "usage: compare_versions.py <current-version> <release-version>",
            file=sys.stderr,
        )
        return 2

    try:
        current_version = parse_version(sys.argv[1])
        release_version = parse_version(sys.argv[2])
    except ValueError as e:
        print(f"failed to parse version: {e}", file=sys.stderr)
        return 2

    print("true" if release_version >= current_version else "false")
    return 0


if __name__ == "__main__":
    sys.exit(main())
