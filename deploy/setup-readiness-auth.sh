#!/usr/bin/env bash
# IDトークンの失効・削除確認に必要な読み取りだけを実行用SAへ付与する。
set -euo pipefail
: "${PROJECT_ID:?Set PROJECT_ID}"
runtime_sa=${RUNTIME_SA:-dopagaki-runtime@$PROJECT_ID.iam.gserviceaccount.com}
role_id=dopagakiFirebaseSessionVerification
role="projects/$PROJECT_ID/roles/$role_id"
cloud=(gcloud --project="$PROJECT_ID" --quiet)
existing=$("${cloud[@]}" iam roles list --filter="name=$role" --format=json)
if [[ $(jq 'length' <<< "$existing") == 0 ]]; then
  "${cloud[@]}" iam roles create "$role_id" \
    --title='DOPAGAKI Firebase session verification' \
    --description='Read Firebase users to check revoked or deleted authentication sessions' \
    --permissions=firebaseauth.users.get --stage=GA > /dev/null
else
  "${cloud[@]}" iam roles describe "$role_id" --format=json |
    jq -e '.deleted != true and (.includedPermissions == ["firebaseauth.users.get"])' > /dev/null
fi
"${cloud[@]}" projects add-iam-policy-binding "$PROJECT_ID" \
  --member="serviceAccount:$runtime_sa" --role="$role" --condition=None > /dev/null
