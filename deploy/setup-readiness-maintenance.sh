#!/usr/bin/env bash
set -euo pipefail
: "${PROJECT_ID:?Set PROJECT_ID}"
: "${REGION:?Set REGION}"
: "${APP_URL:?Set APP_URL}"
: "${SCHEDULER_INVOKER:?Set SCHEDULER_INVOKER}"
job=dopagaki-account-cleanup
flags=(--project="$PROJECT_ID" --location="$REGION" --schedule='* * * * *' --time-zone='Etc/UTC' --uri="${APP_URL%/}/api/internal/account/cleanup" --http-method=POST --oidc-service-account-email="$SCHEDULER_INVOKER" --oidc-token-audience="${OIDC_AUDIENCE:-${APP_URL%/}}" --attempt-deadline=60s)
if gcloud scheduler jobs describe "$job" --project="$PROJECT_ID" --location="$REGION" >/dev/null 2>&1;then
 gcloud scheduler jobs update http "$job" "${flags[@]}"
else
 gcloud scheduler jobs create http "$job" "${flags[@]}"
fi
gcloud firestore fields ttls update expiresAt --project="$PROJECT_ID" --collection-group=accountJobs --enable-ttl --async
