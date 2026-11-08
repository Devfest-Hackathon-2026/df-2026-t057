# Terraform

本番（`devfest-2026-513816`）の Google Cloud の設定を、このディレクトリで管理する。値は `terraform.tfvars` にある。

## 管理しているもの

| ファイル | 中身 |
|---|---|
| `services.tf` | API が使う Google Cloud の API の有効化 |
| `iam.tf` | サービスアカウント4つ、カスタムロール5つ、実行 SA のプロジェクト権限 |
| `run.tf` | Cloud Run `dopagaki`（環境変数・Secret 参照・スケール）、Artifact Registry、呼び出し権限 |
| `outbox.tf` | Cloud Tasks のキュー `dopagaki-outbox`、Cloud Scheduler の `dopagaki-calendar-renew` |
| `github.tf` | GitHub Actions から配信するための Workload Identity Federation |
| `secrets.tf` | Secret `dopagaki-calendar-oauth` の器（値は入れない） |
| `firestore.tf` | Firestore のデータベース、TTL、インデックスの除外、ルール（`../firestore.rules`） |

管理していないもの：

- Cloud Run のイメージとトラフィック。CD（`../cloud-run.sh`）が配信のたびに変える。
- Secret の値。state に秘密の値を入れないため、`gcloud secrets versions add` で入れる。
- 利用者ごとの Calendar トークン（`dopagaki-calendar-<uid>`）。API が作って消す。
- Firebase Authentication の設定、OAuth クライアント、Google Chat アプリ。画面で設定した。
- Google が管理するサービスアカウントの権限（`compute@developer`、`firebase-adminsdk`）。
- Secret `dopagaki-scheduler`。Scheduler を OIDC に切り替えてから使っていない。

プロジェクトの IAM は `google_project_iam_member` で1つずつ追加する形にしている。ここに書いていないほかの人やサービスの権限は、Terraform が消さない。

## 最初の準備

state を置くバケットを一度だけ作る。Terraform で管理すると、state を置く場所がないうちに state が必要になるので、ここだけ gcloud で作る。

```bash
gcloud storage buckets create gs://devfest-2026-513816-tfstate \
  --project=devfest-2026-513816 --location=asia-northeast1 \
  --uniform-bucket-level-access --public-access-prevention
gcloud storage buckets update gs://devfest-2026-513816-tfstate --versioning
```

## 実行のしかた

Terraform のバージョンは `mise.toml` を正とする。プロジェクトの IAM を変えられるアカウントで実行する。

```bash
mise install
gcloud auth application-default login
cd deploy/terraform
terraform init
terraform plan
terraform apply
```

state は GCS にあり、同時に2人が apply しないよう Terraform がロックする。

## 既存のリソースの取り込み

本番のリソースは、Terraform を入れる前に gcloud と画面で作ってあった。2026-11-08に `import` ブロックで、作り直さずに state へ取り込んだ。取り込みが済んだので `import` ブロックは消してある。経緯と plan の結果は [engineering/terraform-2026-11-08.md](../../engineering/terraform-2026-11-08.md) にある。

## Cloud Run の配信との分担

環境変数とリソースの設定は Terraform、イメージとトラフィックは CD が変える。

- CD は、確認を通したリビジョンにトラフィックを固定する。そのため、Terraform で環境変数を変えて apply すると新しいリビジョンはできるが、トラフィックは前のリビジョンに流れたまま。次の CD の配信で新しい設定が使われる。すぐに切り替えるときは、新しいリビジョンを確かめてから `gcloud run services update-traffic dopagaki --region=asia-northeast1 --to-latest` を実行する。
- CD はリビジョン名を付けて配信するので、CD のあとの `terraform plan` には `template.revision` の差分が出る。apply しても同じ設定のリビジョンが1つ増えるだけで、トラフィックは変わらない。
