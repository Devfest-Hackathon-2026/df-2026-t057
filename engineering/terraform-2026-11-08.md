# Terraformへの移行

記録日：2026-11-08。対象は Google Cloud プロジェクト `devfest-2026-513816`（番号 `1032251723022`）。コードは [deploy/terraform](../deploy/terraform/README.md)。

**状態：2026-11-08に本番へ apply し、既存のリソースを state へ取り込んだ。apply 後の plan は差分なし。**

## 背景

これまで Google Cloud の設定は、`deploy/setup-cd.sh`、`deploy/setup-cloud-tasks.sh`、`deploy/iam/` の手順、画面での操作で作っていた。スクリプトは作るときの手順で、本番の今の状態とコードのずれを確かめる方法がなかった。作り直し（PR #29）のときに、Terraform への移行を次の別PRへ後回しにしていた。

## 決めたこと

| 項目 | 選んだもの | 理由 |
|---|---|---|
| state の置き場所 | GCS バケット `devfest-2026-513816-tfstate`（版の保存あり） | 複数人で使え、Terraform がロックする。手元のファイルではこの Mac でしか実行できない |
| Cloud Run の範囲 | SA・リソース・スケール・環境変数・Secret 参照まで。イメージとトラフィックは無視 | 環境変数をコードで確かめられるようにする。イメージとトラフィックは CD が候補確認のうえで変える |
| プロジェクトの IAM | `google_project_iam_member`（1つずつ追加） | `google_project_iam_policy` や `_binding` は、ここに書いていない権限を消す |
| 既存リソース | `import` ブロックで取り込む | 作り直すと、Cloud Run の URL、SA の ID、Secret の版が変わる |
| 古いスクリプト | 消した（`setup-cd.sh`、`setup-cloud-tasks.sh`、`iam/`、`firestore.indexes.json`、`cloudrun.env.example.yaml`） | 同じ設定が2か所にあると、ずれる。当時の手順は git の履歴に残る |
| Secret の値 | Terraform に入れない | state に秘密の値が残る |
| バージョン | Terraform 1.17.1、google provider 8.10.0 | 2026-11-08時点の最新。`mise.toml` と `.terraform.lock.hcl` で固定した |

見送った案：

- **Cloud Run のトラフィックも Terraform で管理する。** CD の `deploy/cloud-run.sh` は、確認を通したリビジョン名にトラフィックを固定する。Terraform が最新のリビジョンへ流すようにすると、CD の確認を通さずに切り替わる。コードフリーズ（11/10）の前に CD の手順を変えないことにした。
- **`template.revision` を無視する。** 無視すると、環境変数を変える apply で古いリビジョン名を送り、「同じ名前のリビジョンがある」と拒否される。そのため、CD のあとの plan に revision の差分が出るのは受け入れた。

## 管理しないもの

Firebase Authentication の設定、OAuth クライアント、Google Chat アプリは画面で設定したもので、google provider にリソースがないか、作成時の同意が必要なため管理しない。利用者ごとの Calendar トークンの Secret は API が作って消す。Google が管理する SA（`compute@developer` の `roles/editor`、`firebase-adminsdk`）は扱わない。

Secret `dopagaki-scheduler` は、Scheduler を OIDC に切り替えてからコードで使っていない（`git grep SCHEDULER_SECRET` で該当なし）。消すかどうかは別に判断するので、Terraform に入れていない。実行 SA の読み取り条件には、本番と同じくこの名前が残っている。

## 本番に対する plan（2026-11-08）

state を手元の一時ファイルに置き、本番の読み取りだけで `terraform plan` を実行した。結果は `52 to import, 1 to add, 16 to change, 0 to destroy`。

取り込んだ52件は、API 13、SA 4、カスタムロール 5、プロジェクトの IAM 8、SA の IAM 3、Artifact Registry と IAM 2、Cloud Run と IAM 4、Cloud Tasks と IAM 2、Scheduler 1、WIF 2、Secret 1、Firestore のデータベース・項目・ルール 7。Cloud Run の環境変数・スケール・リソース、キューの再試行、WIF の条件、ルールの本文は、本番と差分がなかった。

| 差分 | 内容 | 扱い |
|---|---|---|
| Cloud Run の `template.revision` | CD が付けたリビジョン名がなくなる | apply すると同じ設定のリビジョンが1つ増える。トラフィックは変わらない |
| Scheduler の `Authorization` ヘッダー | OIDC に切り替える前の固定のトークンがジョブに残っていた | apply で消える。API は OIDC で確かめるので、動きは変わらない見込み（未確認） |
| `accountJobs.expiresAt` の TTL | `firestore.indexes.json` にはあったが、`setup-cloud-tasks.sh` が設定していなかった | apply で作る |
| `deletion_policy`（14件） | Terraform の中だけの設定 | Google Cloud 側は変わらない |

Firebase Rules の API は、個人のアカウントで呼ぶと課金先のプロジェクトの指定を求めて 403 を返した。provider に `user_project_override` と `billing_project` を設定して通した。

## 本番への apply（2026-11-08）

state のバケットを [README](../deploy/terraform/README.md#最初の準備) の手順で作り、GCS の backend で上と同じ plan が出ることを確かめてから apply した。apply 後の state は53件（取り込み52件と、`accountJobs` の TTL）。

`imports.tf` を消したあとの `terraform plan` は `No changes` だった。apply で Terraform が名前を付けずにリビジョンを作ったので、`template.revision` の差分もなくなった。次の CD の配信のあとには、また出る。

apply の後に、本番を読み取りで確かめた。

| 対象 | 結果 |
|---|---|
| Cloud Run | apply で `dopagaki-00057-gim` と `dopagaki-00037-c8x` の2つのリビジョンができ、どちらも Ready でトラフィックなし。2つできた理由は確かめていない。トラフィックは apply 前と同じ `dopagaki-00055-tuq` が100% |
| 公開 URL | `/api/health` が200 |
| Scheduler | ヘッダーは `User-Agent` だけになり、OIDC（`dopagaki-invoker`）は残っている。apply 後の実行はまだない |
| Firestore | `accountJobs.expiresAt` の TTL が `ACTIVE` |

## 残りの作業

1. Scheduler の `dopagaki-calendar-renew` が、ヘッダーを消したあとも成功することを確かめる（次の定期実行か、手動の実行で）。
2. Secret `dopagaki-scheduler` を消すか決める。
