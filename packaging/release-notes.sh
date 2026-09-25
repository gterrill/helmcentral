#!/bin/sh
# Print the CHANGELOG.md section for one release tag, for use as the GitHub
# release body (goreleaser --release-notes).
#
#   sh packaging/release-notes.sh v0.31.0 [CHANGELOG.md]
#
# Exits non-zero when the tag has no section, or the section is empty, so a
# release cannot publish without written notes.
set -eu

tag="${1:?usage: release-notes.sh <tag> [changelog]}"
changelog="${2:-CHANGELOG.md}"
version="${tag#v}"

notes=$(awk -v heading="## [$version]" '
  index($0, heading) == 1 { found = 1; next }
  found && /^## \[/ { exit }
  found && /^\[[^]]+\]: / { exit }
  found { print }
' "$changelog" | sed -e '/./,$!d')

if [ -z "$(printf '%s' "$notes" | tr -d '[:space:]')" ]; then
  echo "release-notes: no \"## [$version]\" section with content in $changelog" >&2
  exit 1
fi

printf '%s\n' "$notes"
