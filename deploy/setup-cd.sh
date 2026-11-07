#!/usr/bin/env bash
set -euo pipefail

# One-time setup for this repository and existing Cloud Run service.
project=devfest-2026-513816
deployer=dopagaki-deploy@devfest-2026-513816.iam.gserviceaccount.com
cloud=(gcloud --project="$project" --billing-project="$project" --quiet)

"${cloud[@]}" services enable iam.googleapis.com iamcredentials.googleapis.com sts.googleapis.com
"${cloud[@]}" iam service-accounts create dopagaki-deploy --display-name='DOPAGAKI GitHub deployer'
"${cloud[@]}" iam workload-identity-pools create dopagaki-github --location=global \
  --display-name='DOPAGAKI GitHub Actions'
"${cloud[@]}" iam workload-identity-pools providers create-oidc github \
  --location=global --workload-identity-pool=dopagaki-github \
  --issuer-uri=https://token.actions.githubusercontent.com \
  --attribute-mapping='google.subject=assertion.sub,attribute.repository_id=assertion.repository_id,attribute.repository_owner_id=assertion.repository_owner_id,attribute.ref=assertion.ref,attribute.workflow_ref=assertion.workflow_ref,attribute.event_name=assertion.event_name' \
  --attribute-condition="assertion.repository_id == '1446515147' && assertion.repository_owner_id == '281961570' && assertion.ref == 'refs/heads/main' && assertion.workflow_ref == 'a-sumikawa/devfest-2026/.github/workflows/check.yml@refs/heads/main' && assertion.event_name == 'push'"

"${cloud[@]}" iam service-accounts add-iam-policy-binding "$deployer" \
  --role=roles/iam.workloadIdentityUser \
  --member='principalSet://iam.googleapis.com/projects/1032251723022/locations/global/workloadIdentityPools/dopagaki-github/attribute.repository_id/1446515147'
"${cloud[@]}" artifacts repositories add-iam-policy-binding dopagaki --location=asia-northeast1 \
  --member="serviceAccount:$deployer" --role=roles/artifactregistry.writer
"${cloud[@]}" run services add-iam-policy-binding dopagaki --region=asia-northeast1 \
  --member="serviceAccount:$deployer" --role=roles/run.developer
"${cloud[@]}" iam service-accounts add-iam-policy-binding \
  dopagaki-runtime@devfest-2026-513816.iam.gserviceaccount.com \
  --member="serviceAccount:$deployer" --role=roles/iam.serviceAccountUser
