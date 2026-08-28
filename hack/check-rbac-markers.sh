#!/usr/bin/env bash
# Fail if any +kubebuilder:rbac marker block is attached to the declaration below it.
#
# +kubebuilder:rbac is a package-level marker in controller-tools. It is collected
# only from DETACHED comment blocks. A block sitting directly above a declaration
# becomes that declaration's doc comment, which the package-level collector never
# reads - so the marker silently generates nothing and looks correct in review.
#
# `make manifests` + `git diff --exit-code` cannot catch this: an inert marker
# produces no output, so the diff stays clean. Hence this separate check.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 1

found=0
if ! files=$(mktemp); then
  printf 'FAIL: cannot create temporary file for Go file scan\n' >&2
  exit 1
fi
if ! marker_lines=$(mktemp); then
  rm -f "$files"
  printf 'FAIL: cannot create temporary file for marker scan\n' >&2
  exit 1
fi
trap 'rm -f "$files" "$marker_lines"' EXIT

grep -rl '+kubebuilder:rbac' --include='*.go' . >"$files"
grep_status=$?
if [ "$grep_status" -gt 1 ]; then
  printf 'FAIL: cannot scan Go files for RBAC markers (grep exited %d)\n' "$grep_status" >&2
  exit 1
fi

while IFS= read -r f; do
  [ -n "$f" ] || continue

  awk '
    # Track the COMMENT GROUP, not the marker line. A marker group may contain
    # ordinary prose comments between markers; controller-gen still collects it
    # so long as the whole group is detached from the declaration below.
    /^[[:space:]]*\/\// {
      if (/\+kubebuilder:rbac/ && !start) start = NR
      incomment = 1
      next
    }
    {
      if (incomment && start && $0 !~ /^[[:space:]]*$/) print start
      incomment = 0; start = 0
    }
  ' "$f" >"$marker_lines"
  awk_status=$?
  if [ "$awk_status" -ne 0 ]; then
    printf 'FAIL: cannot scan %s for inert RBAC markers (awk exited %d)\n' "$f" "$awk_status" >&2
    exit 1
  fi

  while IFS= read -r ln; do
    [ -n "$ln" ] || continue
    printf '%s:%s: rbac marker block is attached to the declaration below it; add a blank line\n' "$f" "$ln"
    found=$((found + 1))
  done < "$marker_lines"
done < "$files"

if [ "$found" -ne 0 ]; then
  printf '\nFAIL: %d inert +kubebuilder:rbac marker block(s). controller-gen will not see them.\n' "$found"
  exit 1
fi
echo "OK: all +kubebuilder:rbac marker blocks are detached."
