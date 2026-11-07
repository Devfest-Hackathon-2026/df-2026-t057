# 実行アカウントの権限縮小

2026-11-07の読み取り確認に基づく、`dopagaki-runtime` だけの権限縮小。ユーザー承認後に本番へ適用し、同じ実行SAでSDK操作とAI疎通を確認した。[適用結果](../../engineering/security-rollout-2026-11-07.md)を参照。以下は当時の準備・適用・戻し方であり、適用済みの状態へそのまま再実行しない。

| 適用前 | 適用後 |
|---|---|
| `roles/aiplatform.user`（確認時466権限） | `runtime-ai-role.yaml` の推論1権限 |
| `roles/datastore.user`（確認時23権限） | `runtime-data-role.yaml` のデータ操作6権限 |
| `dopagaki-` 接頭辞のSecretを読み取り | Calendar用の名前空間とSchedulerのSecretへ限定 |
| `dopagaki-` 接頭辞のSecretへバージョン追加 | Calendar用の名前空間から静的OAuth Secretを除外。Schedulerも対象外 |

`secretmanager.secrets.create` のカスタムロールは保持する。作成前のSecret名への同じ条件付けができるとは扱わない。FirestoreのIAMはデータベース内のユーザー別分離を担わず、API側の検証済みUIDで分離する。

権限の根拠：[Gemini generateContent](https://docs.cloud.google.com/gemini-enterprise-agent-platform/reference/rest/v1/projects.locations.endpoints/generateContent)、[Firestore IAM](https://cloud.google.com/firestore/native/docs/security/iam)、[Secret Manager IAM Conditions](https://cloud.google.com/secret-manager/docs/access-control#iam_conditions)。

## 変更ファイルの準備

以下はポリシーの取得とローカルファイルの作成だけを行う。ほかのメンバーやロール・条件を保持し、実行SAの対象バインディングだけを書き換える。現在のetagを保持するため、取得後にほかの変更が入った場合、適用は競合として拒否される。

```bash
mkdir -p .local
gcloud projects get-iam-policy devfest-2026-513816 --format=json > .local/iam-policy-before.json
python3 deploy/iam/prepare-policy.py .local/iam-policy-before.json .local/iam-policy-proposed.json
```

## 承認後の適用

実行前に、現在のロール定義と変更する本人のバインディングを再確認する。既存の同名カスタムロールがある場合は、その定義を照合してから更新する。

```bash
gcloud iam roles create dopagakiGeminiInference \
  --project=devfest-2026-513816 \
  --file=deploy/iam/runtime-ai-role.yaml
gcloud iam roles create dopagakiDataAccess \
  --project=devfest-2026-513816 \
  --file=deploy/iam/runtime-data-role.yaml
gcloud projects set-iam-policy devfest-2026-513816 .local/iam-policy-proposed.json
```

適用後は新規利用者の初期データ作成、課題・相談の読み書き、AI推論、ユーザー別Calendar Secretの作成・追加・読み取りを確認する。有料APIや実OAuth同意を伴う確認は、その操作の承認を得てから行う。`/api/health` の200だけでは権限縮小の成功と扱わない。

## ロールバック

最新ポリシーを取得し、実行SAの対象バインディングだけを保存済みの内容へ戻す。他人の同時変更を古いポリシーで上書きしない。

```bash
gcloud projects get-iam-policy devfest-2026-513816 --format=json > .local/iam-policy-current.json
python3 deploy/iam/prepare-policy.py .local/iam-policy-current.json .local/iam-policy-rollback.json --restore-from .local/iam-policy-before.json
gcloud projects set-iam-policy devfest-2026-513816 .local/iam-policy-rollback.json
```

変更に失敗した場合に、OwnerやEditorを実行SAへ追加して回避しない。
