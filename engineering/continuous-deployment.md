# Cloud Runの継続的デプロイ

設定日：2026-11-07。対象は `a-sumikawa/devfest-2026` と、Google Cloudプロジェクト `devfest-2026-513816`（番号 `1032251723022`）の既存サービス `dopagaki`。Terraformは追加していない。

## 配信の流れ

```mermaid
flowchart TD
  PR[Pull request] --> Check[変更範囲に応じた検証]
  Check --> Merge[mainへマージ]
  Merge --> MainCheck[mainの変更を検証]
  MainCheck --> Scope{配信対象の変更か}
  Scope -->|資料や拡張のみ| Skip[配信を省略]
  Scope -->|Web・API・共通型・配信設定| Build[GitHub ActionsでDockerイメージを作成]
  Build --> Auth[GitHub OIDC / Workload Identity Federation]
  Auth --> Push[既存Artifact Registryへ保存]
  Push --> Digest[イメージのdigestを固定]
  Digest --> Candidate[Cloud Runへトラフィック0で配信]
  Candidate --> Smoke{候補のhealth 200 / 未認証state 401}
  Smoke -->|失敗| Stop[現行の本番トラフィックを維持して失敗終了]
  Smoke -->|成功| Promote[候補リビジョンへ100%切替]
  Promote --> Verify{公開URLを同じ条件で確認}
  Verify -->|成功| Done[revision・digest・URLを実行結果へ記録]
  Verify -->|失敗| Rollback[切替前のリビジョンと配分へ戻して失敗終了]
```

[check.yml](../.github/workflows/check.yml) は、最新mainの配信対象変更について本番用イメージを一度だけ作る。デモモードのコンテナーで全E2Eを通した後、圧縮した同じイメージとイメージIDを3日保存のartifactで[cd.yml](../.github/workflows/cd.yml)へ渡す。CDは同じワークフロー実行のartifact IDで取得し、ロード後のイメージIDが一致した場合だけ既存のレジストリへpushする。CDでの再ビルドは行わない。レジストリのdigestを指定する配信前後の確認と復旧は `deploy/cloud-run.sh` を維持する。

PRでは、ビルド設定・依存や受け渡しを変えた場合にコンテナーを検証するが、GoogleのOIDC認証・レジストリへのpush・本番更新は行わない。通常のアプリPRは必要なビルドとスモーク、重要な変更は全E2Eを実行する。

mainへのpushでも、資料・テスト・CI専用の変更では本番配信しない。API/Web/関連共通パッケージの実行コード、イメージの入力、CDワークフローや配信スクリプトの変更を配信対象にする。拡張だけの実行コードは対象外だが、Dockerfileが参照するworkspaceの依存manifest・lockの変更はビルド入力として保守的に扱う。

古いCIはジョブ単位で取り消し、手動検証とmainの自動検証の取消グループは分ける。集約checkとCD開始時にGitHubの最新mainを照合する。CDは `dopagaki-production` グループで直列実行し、開始済みの配信は新しいpushで中断しない。中間のすべてのコミットを必ず配信する方式ではない。

## 認証と権限

サービスアカウントのJSON鍵は作らず、GitHub SecretsにもGoogleの鍵を置かない。公式の[auth Action](https://github.com/google-github-actions/auth)と[Workload Identity Federation](https://docs.cloud.google.com/iam/docs/workload-identity-federation-with-deployment-pipelines)を使う。

| リソース | 設定 |
|---|---|
| Pool | `projects/1032251723022/locations/global/workloadIdentityPools/dopagaki-github` |
| Provider | 同Poolの `providers/github`、issuerは `https://token.actions.githubusercontent.com` |
| 配信SA | `dopagaki-deploy@devfest-2026-513816.iam.gserviceaccount.com` |
| Artifact Registryの `dopagaki` リポジトリ | 配信SAへ `roles/artifactregistry.writer` |
| Cloud Runの `dopagaki` サービス | 配信SAへ `roles/run.developer` |
| 実行SA `dopagaki-runtime` | 配信SAへ `roles/iam.serviceAccountUser` |
| 配信SAの偽装 | 当該Pool内のrepository_id `1446515147` だけに `roles/iam.workloadIdentityUser` |

Providerは次をすべて満たすトークンだけ受け付ける。

```text
repository_id       = 1446515147
repository_owner_id = 281961570
ref                 = refs/heads/main
workflow_ref        = a-sumikawa/devfest-2026/.github/workflows/check.yml@refs/heads/main
event_name          = push
```

名前の再利用やfork、PR、別ブランチ、別ワークフローからの配信SA利用を防ぐため、数値のリポジトリ・所有者IDとmainのワークフローを併せて制限した。ワークフローの移動・リポジトリ移管・所有者変更時は、この条件も見直す。mainの書き込み権限を持つ人は、最終的に実行SAの権限で動くコードを配信できる。配信SA自体へFirestoreやSecret Managerの直接アクセス権を付けていないことを、アプリコードからの間接アクセスまで防いでいるという意味には使わない。

Googleの[Cloud Run配信の権限](https://docs.cloud.google.com/run/docs/deploying)に従い、Cloud Run・Artifact Registry・実行SAの各リソースに限定した。IAM管理・他サービスへの配信・予算変更は配信ワークフローの権限に含めない。既存のallUsers Invokerと、Firebaseによる個人API認証は維持する。

配信コマンドは各リソースの `--project` を必ず指定する。クォータプロジェクトを明示する `--billing-project` は配信SAのコマンドへ付けず、[サービスアカウントの所属プロジェクトを使う標準動作](https://docs.cloud.google.com/docs/quotas/set-quota-project)に従う。明示指定のためだけにプロジェクト全体の `serviceusage.services.use` 権限を追加しない。

初回設定は当時 `deploy/setup-cd.sh` で行った。2026-11-08からは、配信SA・Pool・Provider・4つの権限を [Terraform](../deploy/terraform/github.tf) で管理する（[記録](terraform-2026-11-08.md)）。

## ビルドと切替

- 検証したmainのコミットをチェックアウトし、リポジトリのDockerfileから `linux/amd64` イメージを作る。ローカルの既存Goバイナリーは使わない。
- 長いDockerビルドで短期認証の時間を使い切らないよう、Google認証はビルド後に行う。公式Google Actionsは取得したコミットSHAへ固定した。
- コミットSHAをイメージのタグに使い、実際のCloud Run更新は `@sha256:...` のdigestへ固定する。
- [cloud-run.sh](../deploy/cloud-run.sh) が現行トラフィックを控え、候補を `--no-traffic --tag=cd-candidate` で作る。個人データの認証が外れていないことも確認してから切り替える。
- 切替後に公開URLの確認が失敗した場合、切替前のリビジョンと配分へ戻す。データの変更を巻き戻すものではない。将来のデータ形式変更は、旧版でも読める形で設計する必要がある。
- 候補確認で失敗したリビジョン・タグは調査用に残り、次の実行で候補タグを置き換える。本番配信割合は変えない。
- 変更するのは配信イメージ・リビジョン・タグ・トラフィック。既存のOAuth設定、Secret参照、実行SA、リソース上限、予算設定はスクリプトから変更しない。

ヘルスチェックと未認証拒否は自動で確認する。Geminiへの有料呼び出し、本人のGoogle同意、個人カレンダーの登録、実際のYouTube操作までをCDのたびに行うものではない。API・画面・拡張の回帰確認は、配信前のCIで模擬データを使って行う。

## 停止と手動ロールバック

緊急停止は、配信が始まる前ならGitHub Actionsの対象実行を取り消す。配信済みなら下記で既知のリビジョンへ戻す。配信処理が進行中の場合は状態を確認してから操作する。

```bash
gcloud run services update-traffic dopagaki \
  --to-revisions=KNOWN_GOOD_REVISION=100 \
  --region=asia-northeast1 --project=devfest-2026-513816
```

CDを継続停止する場合はProvider `github` を無効にして新規認証を止め、修正PRを用意する。既に発行された短期トークンの失効と同じ操作ではないため、進行中の実行も確認する。JSON鍵の作成で迂回しない。

## 検証記録

- ローカルの `tests/deploy.test.ts` 6件が成功。候補のhealthまたは認証確認で失敗すると切替がないこと、切替後の確認失敗では元の60%/40%配分まで戻すこと、可変タグの入力を拒否することを確認。外部クラウドや個人データにはアクセスしない。
- `npm run test:unit` は最新mainの拡張ビルド確認を含む11件が成功。`actionlint` v1.7.12、`bash -n deploy/cloud-run.sh deploy/setup-cd.sh`、`git diff --check` も成功。
- GCPでSTS APIの有効化、配信SA・Pool・Providerの作成、対象リソースへの限定した権限追加は完了。
- PR上ではDockerイメージのビルドまで検証し、配信は行わない。mainへの反映とGitHub発の初回CDは、下記のとおり成功した。


## 最新mainへの統合（2026-11-07）

本番のセキュリティ修正を含むPR #11と、拡張のRelease設定を含むPR #12がmainへ入ったため、既存のCD用PR #6へ最新main `2be9301` を取り込んだ。アプリとDockerfileは最新mainの内容を使う。既存のOIDC Providerと配信SAの権限を読み取り、有効な状態と当該リポジトリ・main・指定ワークフローへの制限を確認した。セットアップスクリプトの再実行や鍵の作成は行っていない。

PR #6をmainへマージし、そのpushで初回のGitHub発配信が成功した。


## 初回CDの実確認（2026-11-07）

[GitHub Actionsの実行](https://github.com/a-sumikawa/devfest-2026/actions/runs/42353060011)で、main `7fabe1f346d405d2978d663a7876f329e7b88929` の `check` と `deploy` が成功した。単体11件、E2E17件、Go race/vet、Dockerfileからの本番イメージ作成を確認した。GoogleのOIDC認証、Artifact Registryへのpush、候補確認と本番切替はすべてGitHub Actions上で実行された。

- 配信リビジョン：`dopagaki-00021-gus`、100%。
- 配信イメージ：`asia-northeast1-docker.pkg.dev/devfest-2026-513816/dopagaki/app@sha256:ea97b9ca5f8f523efbd2449a04164c668169bb81426a570125854a65871cc5a4`。
- イメージのコミットタグから引いたdigestが、Cloud Runの配信digestと一致。
- 実行SA、環境変数・Secret参照、リソース・タイムアウト・同時実行数の設定は配信前と一致。
- 公開URLのhealthは200、未認証stateは401。新UIのJS・CSS・アイコンフォントは以前検証した成果物とSHA-256が一致し、CSP nonce・HTMLのno-store・privacyの制限も維持されている。

[機械可読な確認記録](evidence/cd-first-deployment-2026-11-07.json)には秘密値や本人のデータを含めない。失敗時の無切替・復旧は模擬テストで確認しており、本番では正常系の配信とHTTP確認を行った。

## スキップされた検査による配信停止（2026-11-10）

mainへのPR #66ではAPI・フロント・E2E・集約checkが成功し、検査済みイメージも保存されたが、対象外のTerraform検査がskipされた影響で配信ジョブまでskipされた。GitHub Actionsの暗黙の`success()`を避けるため、配信条件へ`!cancelled()`を明示した。集約checkの成功、最新main、配信対象の判定は引き続き必須とし、失敗・取消・古いmainからは配信しない。祖先ジョブのskipを再現する回帰テストを追加した。
