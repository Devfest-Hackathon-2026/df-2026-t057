#!/usr/bin/env bash
set -euo pipefail
: "${GITHUB_SHA:?}" "${GITHUB_OUTPUT:?}"
image="dopagaki:$GITHUB_SHA"
image_id=$(docker image inspect --format '{{.Id}}' "$image")
mkdir -p dist
docker save "$image" | gzip -1 > dist/production-image.tar.gz
echo "image-id=$image_id" >> "$GITHUB_OUTPUT"
