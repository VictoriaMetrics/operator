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
cd "$(dirname "$0")/.."

found=0
while IFS= read -r f; do
  [ -n "$f" ] || continue
  while IFS= read -r ln; do
    [ -n "$ln" ] || continue
    printf '%s:%s: rbac marker block is attached to the declaration below it; add a blank line\n' "$f" "$ln"
    found=$((found + 1))
  done < <(awk '
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
  ' "$f")
done < <(grep -rl '+kubebuilder:rbac' --include='*.go' . 2>/dev/null || true)

if [ "$found" -ne 0 ]; then
  printf '\nFAIL: %d inert +kubebuilder:rbac marker block(s). controller-gen will not see them.\n' "$found"
  exit 1
fi
echo "OK: all +kubebuilder:rbac marker blocks are detached."
