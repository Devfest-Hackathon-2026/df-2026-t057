# 運用とCI/CDの現状

記録日：2026-11-06。以下のクラウド設定は配信時に確認した値。秘密情報の実値は含めない。

最新配信は2026-11-07の `dopagaki-00021-gus`（100%）、main `7fabe1f`。PR #6のマージを起点にGitHub Actionsの初回CDが成功した。[自動配信の実確認](continuous-deployment.md#初回cdの実確認2026-11-07)と[機械可読な記録](evidence/cd-first-deployment-2026-11-07.json)を参照。直前はUIを手動配信した `dopagaki-ui-063109e`。

直前の配信は `dopagaki-main-b6dffb4`、main `b6dffb4`。[配信と実Shortsの検証](shorts-verification-2026-11-07.md)を参照。以下の初期配信時の設定・権限は当時の記録として保持する。

前回の配信は管理画面のAI相談を追加した Cloud Run `dopagaki-00013-vtd`（100%）、コード `7b77026`。[変更と実確認の範囲](assistant.md)を参照。Google Chatアプリは無効化し、旧受付APIと専用環境変数を削除した。本人のGoogleアカウント照合とアカウント切替は[一般ユーザー対応の記録](public-accounts.md#配信と実確認)を参照。下記の「初期配信時の環境」と旧digestの配信例は、最初の実サービス確認時点の記録として保持する。

## 整備状況

| 項目 | 状態 | 根拠・範囲 |
|---|---|---|
| CI | 整備済み（変更範囲による実行選択を追加） | [.github/workflows/check.yml](../.github/workflows/check.yml)。PRとmainへのpushで実行。下記の範囲に応じてチェックを選択 |
| Chrome拡張の配布 | タグからReleaseを作成するワークフローを追加 | [release-extension.yml](../.github/workflows/release-extension.yml)。`vX.Y.Z` のpushで型チェック・単体テスト・公開先向けビルド・ZIPとチェックサムの公開を実行。導入は[INSTALL.txt](../extension/INSTALL.txt)参照。初回のGitHub上の実行結果はPR・Actionsで確認 |
| Cloud Runへの配信 | 自動配信の初回成功を確認済み | GitHub Actionsでイメージ作成・push・候補確認・切替 |
| CD | 稼働済み | [継続的デプロイ](continuous-deployment.md)。mainの検証成功後に配信し、公開確認失敗時は元の配分へ戻す |
| Terraform / IaC | 本番へ適用済み | [deploy/terraform](../deploy/terraform/README.md)。2026-11-08に既存のリソースを取り込み、apply 後の plan は差分なし。[記録](terraform-2026-11-08.md) |
| 配信設定の共有 | 一部あり | Dockerfile、Terraform（Cloud Runの環境変数を含む）、Firestoreルール、README |
| 再現可能な一連の運用 | 未完成 | Firebase・OAuth・Chat・IAM等を含む初期設定全体はCLI・画面操作に依存 |

2026-11-07にGitHub ActionsからのCDをmainへ反映し、対象リソースに限定した鍵なし認証で初回配信が成功した。以下の図は以前の手動配信の経路として残す。

```mermaid
flowchart LR
  Change[コミット / PR] --> CI[GitHub Actions: build / test / vet / E2E]
  Source[配信用のソースまたは検証済み成果物] --> Manual[手動でCloud Buildを実行]
  Manual --> Registry[Artifact Registry]
  Registry --> Deploy[手動でCloud Runを更新]
  Deploy --> Smoke[実サービスでの確認]
```

自動配信の経路、認証条件、候補の確認・切替・ロールバックは[継続的デプロイ](continuous-deployment.md)を参照。初回の実行結果も同文書に記録した。Chrome拡張のタグからGitHub Releasesへの自動公開はCloud Run配信とは別のワークフローで、インストール済み拡張の自動更新は行わない。

## CIの実行範囲

変更範囲は `.github/ci-paths.yaml`、実行頻度と配信対象は `deploy/ci-policy.mjs` で判定する。`api` と `frontend` を並列実行し、成功後に `e2e` へ進む。`check` は依存ジョブの失敗・取消を失敗として集約し、資料だけの変更にも成功結果を残す。

| 変更 | PR | main |
|---|---|---|
| 通常のアプリ変更 | 関連する型・Lint・単体＋スモークE2E | 関連する型・Lint・単体＋全E2E |
| 認証・報酬・保存・API契約など | PRでも全E2E | 全E2E |
| Go/TS単体テストだけ | 関連する単体。ブラウザー・配信は省略 | 同左 |
| E2E本体・支援コード | 全E2E | 全E2E。アプリの配信は省略 |
| Dockerfile・依存・ビルド設定 | コンテナーを作り、そのイメージで全E2E | 配信用イメージで全E2E後に同じイメージを配信 |
| CIの実行条件・イメージ受け渡し | 全E2E。受け渡しを変えた場合はコンテナーも確認 | CI専用変更は重い検証・配信を省略 |
| CDワークフロー・配信処理 | 回帰テスト。イメージ受け渡し変更はコンテナーでも確認 | 配信対象として実経路を確認 |
| 資料だけ | 判定とcheckだけ | 同左 |

生成物は入力・生成ツール・生成物を変えたときだけ確認する。拡張のみの通常PRは拡張のスモーク1件、通常のアプリPRは登録・未認証・拡張の作業・アクセシビリティのスモーク4件を選ぶ。全件テストを削除せず、重要な変更とmainで維持する。実際の選択件数はテスト追加で変わり得る。

CIはapi/frontend/e2eごとのconcurrencyグループで古い実行を取り消す。CDは別グループで直列化し、開始済みの配信を中断しない。集約checkとCD開始時に最新mainを確認し、古い合格コミットからの配信を抑止する。

Go・pnpm・Dockerのキャッシュは維持する。PRのDockerは復元のみ、mainで保存する。失敗時のE2E証拠と配信用イメージの受け渡しartifactは3日保存する。定期実行は追加せず、手動の全件検証は配信を伴わない。[構成と検証](ci-jobs-2026-11-08.md)を参照。

## 初期配信時の環境

| 項目 | 値 |
|---|---|
| プロジェクトID | `devfest-2026-513816` |
| プロジェクト番号 | `1032251723022` |
| Cloud Runサービス | `dopagaki` |
| Cloud Run / Firestore / Artifact Registryのリージョン | `asia-northeast1` |
| Vertex AIのlocation | `global` |
| 公開URL | https://dopagaki-1032251723022.asia-northeast1.run.app/ |
| 配信コード | `a25837c` |
| リビジョン | `dopagaki-00006-btr` |
| 配信割合 | このリビジョンへ100% |
| CPU / メモリー | 1 CPU / 512MiB |
| 最小 / 最大インスタンス | 0 / 2 |
| 同時実行数 / リクエスト上限時間 | 40 / 60秒 |
| Calendar監視更新 | `dopagaki-calendar-renew`、毎日09:00、`Asia/Tokyo` |

初期確認時の配信イメージ：

```text
asia-northeast1-docker.pkg.dev/devfest-2026-513816/dopagaki/app@sha256:96a02db958e38e38cc56c97510c0f5f020fe75c9cb63d4b446186be1241bce25
```

課金アカウントと予算はユーザーが設定した。この開発で変更していない。予算通知等の設定内容を棚卸しした記録でも、1万円で必ず課金が停止することを検証した記録でもない。

## 初期設定で必要だったもの

1. Cloud Run、Cloud Build、Artifact Registry、Firestore、Firebase Authentication、Secret Manager、Vertex AI、Calendar、Chat、Cloud Scheduler等のAPIを有効にした。
2. 既存のGoogle CloudプロジェクトへFirebaseを追加した。別のプロジェクトは新規作成しなかった。
3. FirebaseのWebアプリとGoogleログインを設定し、公開Cloud Runドメインを認証の許可ドメインへ追加した。
4. OAuthクライアントに公開オリジンと `/api/calendar/callback` を登録した。Firebaseログイン用の既存redirect URIも保持した。
5. Firestore Nativeを用意し、[クライアントルール](../deploy/firestore.rules) を全拒否にした。
6. 実行用とビルド用のサービスアカウントを分けた。
7. Secret ManagerへOAuth client secretとScheduler用Secretを登録した。ユーザー別Calendarトークンは接続時にアプリが保存する。
8. 個人用の未公開Chatアプリを設定し、HTTPの共通エンドポイントを `/api/chat` にした。
9. Calendar監視更新のSchedulerを設定して実行した。

Firebase初回の規約同意、OAuthの個人アカウントの同意、未公開Chatアプリと開発中のChrome拡張の追加では、利用者による確認・操作が発生した。これらも含めた完全無人の初期構築はできていない。

## 権限の分担

| 対象 | 配信時の設定 |
|---|---|
| `dopagaki-runtime` | `roles/datastore.user`、`roles/aiplatform.user` |
| 同上、Secretの作成 | `secretmanager.secrets.create` のみを含むカスタムロール。作成権限はプロジェクト単位 |
| 同上、Secretの読み取り・バージョン追加 | `roles/secretmanager.secretAccessor`、`roles/secretmanager.secretVersionAdder`。`dopagaki-` 接頭辞のリソース名に条件を限定 |
| `dopagaki-build` | `roles/run.builder` |
| Cloud Run `dopagaki` | サービス単位で `allUsers` のInvokerを許可。静的画面と外部からのWebhook受信のため公開 |
| 個人データ用API | 公開HTTP到達性とは別に、Firebaseの認証を検証 |

各SAの完全な名前は `<SA名>@devfest-2026-513816.iam.gserviceaccount.com`。他アプリ・他プロジェクトの権限を変更する作業は対象にしていない。

Secret読み取り・追加の条件は、配信時には次の形式を使った。

```text
resource.name.startsWith('projects/1032251723022/secrets/dopagaki-')
```

接頭辞の条件は作成権限そのものを限定するものではない。この違いを「すべてのSecret操作を接頭辞で制限した」と説明しない。

## OAuth・AI相談・Scheduler

| 設定 | 扱い |
|---|---|
| Calendarスコープ | `openid`、`email`、`https://www.googleapis.com/auth/calendar.app.created`、`https://www.googleapis.com/auth/calendar.events.owned.readonly`（主カレンダーの予定の表示） |
| Calendarコールバック | 公開URLの `/api/calendar/callback` |
| OAuth client secret | `dopagaki-calendar-oauth` をSecret参照で注入 |
| ユーザー別OAuthトークン | `dopagaki-calendar-` で始まるSecret。実値は記録しない |
| AI相談 | Firebase認証済みの `/api/assistant`。会話はFirestoreの `assistantConversations/{uid}/turns` |
| 旧Google Chat | アプリのステータスを「無効」に保存。`POST /api/chat` は404。既存の旧記録・APIサービスは削除していない |
| Scheduler | `POST /api/calendar/renew`。`dopagaki-scheduler` の値をBearerとして使う |

Firebaseログインと、Calendarの継続アクセスへのOAuth同意は別の操作。ログイン済みでも、Calendarの連携・トークン保存・専用カレンダー作成まで終わったとは限らない。

## 手動配信の再現に必要な情報

[Dockerfile](../Dockerfile) はAngularのビルド、Goのビルド、distrolessの非root実行イメージを作る。最初の配信と `3f33f90` の配信では、Cloud Buildでソースからこのコンテナーを作成した。

後半のChat調査・最終修正では、ローカルで `GOOS=linux GOARCH=amd64 CGO_ENABLED=0` のGoバイナリーとAngular成果物を作り、Cloud Buildで実行イメージへ梱包した。配信コードに対するCIは通したが、最終リビジョンの作成経路を「GitHub CIが本番イメージを作って自動配信した」と説明しない。

今後ソースから配信する際のCloud Build設定例は次の形。現在、これが自動で実行される設定はない。

```yaml
steps:
  - name: gcr.io/cloud-builders/docker
    args: [build, -t, '${_IMAGE}', .]
images: ['${_IMAGE}']
options:
  logging: CLOUD_LOGGING_ONLY
timeout: 1200s
```

設定をローカルの `cloudbuild.yaml` に置く。環境変数・Secret参照・リソース上限は [Terraform](../deploy/terraform/run.tf) で設定済みなので、手動配信ではイメージだけを変える。以下は説明用のコマンドで、この記録の作成時には実行していない。

```bash
DOPAGAKI_PROJECT=devfest-2026-513816
DOPAGAKI_REGION=asia-northeast1
DOPAGAKI_TAG=$(git rev-parse --short HEAD)
DOPAGAKI_IMAGE="$DOPAGAKI_REGION-docker.pkg.dev/$DOPAGAKI_PROJECT/dopagaki/app:$DOPAGAKI_TAG"

gcloud builds submit . \
  --config=cloudbuild.yaml \
  --substitutions="_IMAGE=$DOPAGAKI_IMAGE" \
  --service-account="projects/$DOPAGAKI_PROJECT/serviceAccounts/dopagaki-build@$DOPAGAKI_PROJECT.iam.gserviceaccount.com" \
  --region="$DOPAGAKI_REGION" \
  --project="$DOPAGAKI_PROJECT" \
  --billing-project="$DOPAGAKI_PROJECT"
```

本番更新では、完成したイメージのdigestを確認して固定し、`deploy/cloud-run.sh` に渡す（候補確認と切替はCDと同じ）。手動配信後は [検証記録](verification-2026-11-06.md) の範囲で確認する。

次は2026-11-06の初期確認時の配信値で、旧イメージを再配信するコマンド例。新しいビルドを配信する場合は、その結果のdigestへ置き換える。Secretの `:1` も配信時点の値である。

```bash
gcloud run deploy dopagaki \
  --image=asia-northeast1-docker.pkg.dev/devfest-2026-513816/dopagaki/app@sha256:96a02db958e38e38cc56c97510c0f5f020fe75c9cb63d4b446186be1241bce25 \
  --service-account=dopagaki-runtime@devfest-2026-513816.iam.gserviceaccount.com \
  --env-vars-file=.local/cloudrun.env.yaml \
  --update-secrets=SCHEDULER_SECRET=dopagaki-scheduler:1,GOOGLE_OAUTH_CLIENT_SECRET=dopagaki-calendar-oauth:1 \
  --min-instances=0 --max-instances=2 \
  --cpu=1 --memory=512Mi --concurrency=40 --timeout=60 \
  --region=asia-northeast1 \
  --project=devfest-2026-513816 \
  --billing-project=devfest-2026-513816
```

配信に使った一時設定・補助スクリプトは `.local/` にあるが、Git管理していない。これらの存在だけでは第三者が同じ環境を再構築できないことが、現在の運用上の不足。

## TerraformとCDを整備するときに決めること

以下は2026-11-06時点の作業一覧。Terraformの管理範囲・既存環境の取り込み・state管理は、2026-11-08に [Terraformへの移行](terraform-2026-11-08.md) で決めた。

| 項目 | 必要な判断・確認 |
|---|---|
| Terraformの管理範囲 | 既存のRun、Registry、Firestore、SA/IAM、Secretの器、Scheduler等を棚卸しし、リソースごとにprovider対応とimport可否を確認する |
| 既存環境の取り込み | 作成済みのものを重複作成・置換しないよう、import後にplanで差分を確認する |
| state管理 | 保存場所、アクセス権、ロック、運用者を決める。秘密情報をstateへ入れるかも個別に判断する |
| Firebase・OAuth・Chatの画面設定 | どこまでコードで管理できるか未調査。すべてTerraform化できるとはまだ言えない |
| GitHubからGoogle Cloudへの認証 | 認証方式、配信用SA、対象ブランチと権限を決める。現在GitHub向けの配信認証は未設定 |
| 自動配信の起点 | mainマージ、タグ、手動実行等のどれを使うか決める |
| 配信前後の確認 | CI結果と配信イメージの対応、実行環境、ヘルスチェック、失敗時の扱いを定義する |
| ロールバック | アプリのリビジョン切替と、データ・設定の復元を分けて手順化する。現時点で復旧訓練は未実施 |
| 拡張の配布 | GitHub ReleasesでZIPを配布し、Chromeへunpackedで導入する。Store配布とインストール済み拡張の自動更新は未実施 |

この一覧を作ったことでTerraformやCDが整備済みになるわけではない。実装・適用・実行確認が終わったものだけ、上の整備状況を変更する。
