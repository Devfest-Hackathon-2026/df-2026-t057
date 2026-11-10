# AI利用上限の本番反映

記録日：2026-11-10（日本時間）。対象は `a-sumikawa/devfest-2026` と Cloud Run `dopagaki`（`devfest-2026-513816` / `asia-northeast1`）。

## 変更

[PR #63](https://github.com/a-sumikawa/devfest-2026/pull/63)でGoの既定値、Terraform、設定例、READMEを揃え、[PR #70](https://github.com/a-sumikawa/devfest-2026/pull/70)でmain `8a6c5b05b6ff14d1e221082fe0168ea9f64d4f33` へ反映した。

| Gemini呼び出し上限 | 旧コード・設定例 | 新しい値 |
|---|---|---|
| 1人・1分 | 5 | 10 |
| 1人・1日 | 100 | 500 |
| 全体・1分 | 30 | 60 |
| 全体・1日 | 300 | 2000 |

作業開始時に本番の実設定を読むと、すでに4項目とも新しい値だった。本作業で初めて稼働値が増えたという意味ではない。誰がいつ先に更新したかは、この確認では調べていない。

## Terraformの確認と適用

Terraform 1.17.1（公式チェックサム照合済み）、Google provider 8.10.0、既存のGCS backendを使用した。fmtとvalidateは成功した。

全体planは0作成・2変更・0削除で、Cloud Runの `template.revision` の更新に加え、Firestoreの `point_in_time_recovery_enablement` を `ENABLED` から `DISABLED` にする、今回と無関係な差分があった。このplanは適用していない。

今回はCloud Runだけを対象にした保存済みplanを確認してapplyした。Cloud Runのrevision更新1件だけで、4つの上限・他のtemplate設定・トラフィックの変更、リソースの作成・削除はないことをJSONでも検査した。

```bash
# deploy/terraformで、既存の本番設定と認証を用意して実行
terraform fmt -check -recursive
terraform validate
terraform plan -out=/private/tmp/production.tfplan
# 全体planを適用せず、今回の対象に絞る
terraform plan -target=google_cloud_run_v2_service.app -out=/private/tmp/cloud-run.tfplan
terraform apply /private/tmp/cloud-run.tfplan
```

applyは0作成・1変更・0削除で成功した。適用後も `dopagaki-00068-yof` に100%配信され、上限値は表の新しい値と一致した。Firestoreの時点復旧は `POINT_IN_TIME_RECOVERY_ENABLED` のままと確認した。

`-target` は今回の対象外の変更を避けるために使った。全体のTerraform差分が解消したとは扱わない。今後の全体apply前にはFirestoreの復旧設定とコードの差を別途整理する必要がある。CD後に現れるrevision名の差分は、[TerraformとCDの分担](../deploy/terraform/README.md#cloud-run-の配信との分担)を参照する。

## CI/CDと本番確認

[PR #70のCI](https://github.com/a-sumikawa/devfest-2026/actions/runs/42758399370)と、[mainのCI/CD](https://github.com/a-sumikawa/devfest-2026/actions/runs/42758849750)は、API（raceテスト含む）・frontend・Terraform・スモークE2E・総合checkが成功し、mainの配信ジョブも成功した。

2026-11-10 14:04（日本時間）に、Cloud Runサービスだけでなく、実際に100%配信されているリビジョン自身の設定を読み取って確認した。

- 配信リビジョン：`dopagaki-00071-das`、100%。
- 配信コード：main `8a6c5b05b6ff14d1e221082fe0168ea9f64d4f33`。
- イメージdigest：`sha256:2ac17214ab5e2f9b032aa18c76b32d757327b5c9edd32b75f84433deff2ef1dc`。Artifact Registryのmainコミットタグから引いたdigestと、配信中リビジョンのdigestが一致した。
- 4つのAI上限：表の新しい値とすべて一致。
- Firestoreの時点復旧：`POINT_IN_TIME_RECOVERY_ENABLED` を維持。

Cloud Runの公開URLと、環境変数 `APP_URL` に設定されたURLの両方で確認した。未認証拒否の確認には、現行CDと同じ `/api/profile` を使用する。

| URL | `/api/health` | 未認証 `/api/profile` |
|---|---|---|
| `https://dopagaki-auqpl4ga4q-an.a.run.app` | 200、`status: ok` | 401 |
| `https://dopagaki-1032251723022.asia-northeast1.run.app` | 200、`status: ok` | 401 |

[秘密値を含まない確認記録](evidence/ai-quota-rollout-2026-11-10.json)に、照合したコミット・リビジョン・digest・上限・HTTP応答と確認時刻を残した。

## 確認範囲

模擬データの単体テスト・スモークE2Eと、実サービスの設定・配信・HTTP応答の確認を区別する。実Geminiの有料呼び出し、個人カレンダーへの登録、実YouTubeの操作は、この反映確認では行っていない。上限に達するまで実際に呼び出す試験も行っていない。

秘密値、認証トークン、Terraform state、保存済みplan、生のCloud Run設定はこの記録へ載せない。
