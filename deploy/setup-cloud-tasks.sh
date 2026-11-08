#!/usr/bin/env bash
# Outbox（Cloud Tasks）、変更通知のチャンネルの更新（Cloud Scheduler）、Firestore の TTL を用意する。
# 何度実行してもよい（既にあるリソースは作らずに設定だけ合わせる）。Terraform へ移すまでの暫定（AGENTS.md）。
#
# 必須の環境変数:
#   PROJECT_ID  GCP のプロジェクト ID（既定値なし）
#   REGION      Cloud Run・Cloud Tasks・Cloud Scheduler のリージョン（例 asia-northeast1）
#   APP_URL     Cloud Run の公開オリジン（例 https://example.com）。タスクとジョブの宛先・OIDC の audience に使う
# 任意の環境変数:
#   SERVICE      Cloud Run のサービス名（既定 dopagaki）
#   RUNTIME_SA   Cloud Run の実行サービスアカウント（既定 dopagaki-runtime@$PROJECT_ID.iam.gserviceaccount.com）
#   INVOKER_NAME Cloud Tasks・Scheduler が OIDC トークンに使うサービスアカウントの名前（既定 dopagaki-invoker）
#   QUEUE        キューの名前（既定 dopagaki-outbox）
#   OIDC_AUDIENCE OIDC トークンの audience（既定 APP_URL。API の OIDC_AUDIENCE と同じ値にする）
set -euo pipefail

: "${PROJECT_ID:?PROJECT_ID を指定してください}" "${REGION:?REGION を指定してください}" "${APP_URL:?APP_URL を指定してください}"
app_url=${APP_URL%/}
service=${SERVICE:-dopagaki}
runtime_sa=${RUNTIME_SA:-dopagaki-runtime@$PROJECT_ID.iam.gserviceaccount.com}
invoker_name=${INVOKER_NAME:-dopagaki-invoker}
invoker_sa=$invoker_name@$PROJECT_ID.iam.gserviceaccount.com
queue=${QUEUE:-dopagaki-outbox}
audience=${OIDC_AUDIENCE:-$app_url}
scheduler_job=dopagaki-calendar-renew
cloud=(gcloud --project="$PROJECT_ID" --quiet)

"${cloud[@]}" services enable cloudtasks.googleapis.com cloudscheduler.googleapis.com

# Cloud Tasks のキュー。API の Outbox は8回の失敗でジョブを failed にして 2xx を返すので、キューの上限（10回）はそれより大きくする。
queue_flags=(
  --location="$REGION"
  --max-attempts=10
  --min-backoff=10s
  --max-backoff=3600s
  --max-doublings=8
  --max-dispatches-per-second=5
  --max-concurrent-dispatches=10
)
if "${cloud[@]}" tasks queues describe "$queue" --location="$REGION" > /dev/null 2>&1; then
  "${cloud[@]}" tasks queues update "$queue" "${queue_flags[@]}"
else
  "${cloud[@]}" tasks queues create "$queue" "${queue_flags[@]}"
fi

# OIDC トークンに使うサービスアカウント。Cloud Run の呼び出しだけを許す。
if ! "${cloud[@]}" iam service-accounts describe "$invoker_sa" > /dev/null 2>&1; then
  "${cloud[@]}" iam service-accounts create "$invoker_name" --display-name='DOPAGAKI Cloud Tasks / Scheduler invoker'
fi
"${cloud[@]}" run services add-iam-policy-binding "$service" --region="$REGION" \
  --member="serviceAccount:$invoker_sa" --role=roles/run.invoker

# 実行サービスアカウントが、このキューにタスクを積み、タスクに invoker の OIDC トークンを付けられるようにする。
"${cloud[@]}" tasks queues add-iam-policy-binding "$queue" --location="$REGION" \
  --member="serviceAccount:$runtime_sa" --role=roles/cloudtasks.enqueuer
"${cloud[@]}" iam service-accounts add-iam-policy-binding "$invoker_sa" \
  --member="serviceAccount:$runtime_sa" --role=roles/iam.serviceAccountUser

# 毎日、期限が近い変更通知のチャンネルを更新し、実行されないまま残ったジョブを登録し直す。
scheduler_flags=(
  --location="$REGION"
  --schedule='0 4 * * *'
  --time-zone='Etc/UTC'
  --uri="$app_url/api/internal/calendar/renew"
  --http-method=POST
  --oidc-service-account-email="$invoker_sa"
  --oidc-token-audience="$audience"
  --attempt-deadline=300s
)
if "${cloud[@]}" scheduler jobs describe "$scheduler_job" --location="$REGION" > /dev/null 2>&1; then
  "${cloud[@]}" scheduler jobs update http "$scheduler_job" "${scheduler_flags[@]}"
else
  "${cloud[@]}" scheduler jobs create http "$scheduler_job" "${scheduler_flags[@]}"
fi

# Firestore の TTL（expiresAt を過ぎたドキュメントを自動で消す）。定義は deploy/firestore.indexes.json と同じ内容。
for group in idempotency outbox oauthStates; do
  "${cloud[@]}" firestore fields ttls update expiresAt --collection-group="$group" --enable-ttl --async
done

cat <<EOF

Cloud Run に次の環境変数を設定してください:
  CLOUD_TASKS_QUEUE=projects/$PROJECT_ID/locations/$REGION/queues/$queue
  CLOUD_TASKS_INVOKER=$invoker_sa
  SCHEDULER_INVOKER=$invoker_sa
  OIDC_AUDIENCE=$audience

Firestore のインデックスの除外とルールは deploy/firestore.md の手順で反映してください
（firebase deploy --only firestore、または gcloud firestore indexes fields update）。
EOF
