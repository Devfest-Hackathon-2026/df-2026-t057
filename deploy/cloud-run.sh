#!/usr/bin/env bash
set -euo pipefail

: "${PROJECT_ID:?}" "${REGION:?}" "${SERVICE:?}" "${SERVICE_URL:?}"
image=${1:?Pass an immutable image digest}
[[ "$image" == *@sha256:* ]] || { echo 'An image digest is required.' >&2; exit 1; }
cloud=(gcloud --project="$PROJECT_ID" --billing-project="$PROJECT_ID" --quiet)

smoke() {
  local url=$1 status
  curl --fail --silent --show-error --retry 3 --retry-delay 2 --max-time 15 \
    "$url/api/health" | jq -e '.status == "ok"' > /dev/null || return 1
  status=$(curl --silent --show-error --max-time 15 -o /dev/null -w '%{http_code}' "$url/api/state")
  [[ "$status" == 401 ]]
}

# Keep the currently serving revision until the candidate passes both checks.
previous=$(
  "${cloud[@]}" run services describe "$SERVICE" --region="$REGION" --format=json |
    jq -er '[.status.traffic[] | select(.percent > 0) | "\(.revisionName)=\(.percent)"] | join(",") | select(length > 0)'
)
revision=$(
  "${cloud[@]}" run services update "$SERVICE" --region="$REGION" --image="$image" \
    --no-traffic --tag=cd-candidate --format='value(status.latestReadyRevisionName)'
)
candidate=$(
  "${cloud[@]}" run services describe "$SERVICE" --region="$REGION" --format=json |
    jq -er '.status.traffic[] | select(.tag == "cd-candidate") | .url'
)
if ! smoke "$candidate"; then
  echo 'Candidate verification failed; production traffic is unchanged.' >&2
  exit 1
fi

"${cloud[@]}" run services update-traffic "$SERVICE" --region="$REGION" \
  --to-revisions="$revision=100" --remove-tags=cd-candidate
if ! smoke "$SERVICE_URL"; then
  "${cloud[@]}" run services update-traffic "$SERVICE" --region="$REGION" --to-revisions="$previous"
  echo 'Production verification failed; restored previous traffic.' >&2
  exit 1
fi

if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
  {
    echo '### Cloud Run deployment'
    echo "- Revision: \`$revision\`"
    echo "- Image: \`$image\`"
    echo "- URL: $SERVICE_URL"
    echo '- Health: 200; unauthenticated state: 401'
  } >> "$GITHUB_STEP_SUMMARY"
fi
