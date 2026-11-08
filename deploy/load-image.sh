#!/usr/bin/env bash
set -euo pipefail
: "${GITHUB_SHA:?}" "${IMAGE:?}" "${EXPECTED_IMAGE_ID:?}"
gzip -dc dist/production-image.tar.gz | docker load
image="dopagaki:$GITHUB_SHA"
actual=$(docker image inspect --format '{{.Id}}' "$image")
[[ "$actual" == "$EXPECTED_IMAGE_ID" ]] || { echo 'Tested image ID does not match.' >&2; exit 1; }
docker tag "$image" "$IMAGE:$GITHUB_SHA"
