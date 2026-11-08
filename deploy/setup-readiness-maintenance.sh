#!/usr/bin/env bash
set -euo pipefail
: "${PROJECT_ID:?Set PROJECT_ID}"
: "${APP_URL:?Set APP_URL to the public HTTPS origin}"
: "${MAINTENANCE_EMAIL:?Set the dedicated Scheduler service account email}"
case "$APP_URL" in https://*) ;; *) echo 'APP_URL must use HTTPS' >&2; exit 1;; esac
region="${REGION:-asia-northeast1}"
for action in calendar/retry account/cleanup; do
  job="dopagaki-${action//\//-}"
  gcloud scheduler jobs create http "$job" \
    --project="$PROJECT_ID" --location="$region" \
    --schedule='* * * * *' --time-zone=Asia/Tokyo \
    --uri="$APP_URL/api/$action" --http-method=POST \
    --oidc-service-account-email="$MAINTENANCE_EMAIL" \
    --oidc-token-audience="$APP_URL" --attempt-deadline=60s
done

# Only terminal jobs receive expiresAt; pending deletion jobs must not expire.
gcloud firestore fields ttls update expiresAt --collection-group=accountJobs \
  --enable-ttl --project="$PROJECT_ID"
