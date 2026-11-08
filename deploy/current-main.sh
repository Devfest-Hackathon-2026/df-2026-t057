#!/usr/bin/env bash
set -euo pipefail
: "${GITHUB_REPOSITORY:?}" "${GITHUB_SHA:?}" "${GITHUB_OUTPUT:?}"
latest=$(gh api "repos/$GITHUB_REPOSITORY/git/ref/heads/main" --jq '.object.sha')
if [[ "$latest" == "$GITHUB_SHA" ]]; then
  echo 'current=true' >> "$GITHUB_OUTPUT"
else
  echo 'current=false' >> "$GITHUB_OUTPUT"
fi
