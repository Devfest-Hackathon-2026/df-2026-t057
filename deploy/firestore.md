# Firestore のインデックスとルール

ルールは `firestore.rules`、TTL とインデックスの設定は [terraform/firestore.tf](terraform/firestore.tf) が正。

## 何を定義しているか

API の Firestore の検索は、すべて1つの項目の条件と並べ替えだけで書いている（`platform/store` の `Query` の約束）。
そのため **複合インデックスは不要**で、定義していない。検索の一覧:

| コレクション | 条件 | 並べ替え |
|---|---|---|
| `users/{uid}/ledger` | `seq <`（ページ送り）、`at >=`（集計） | `seq`、`at` |
| `users/{uid}/viewingSessions` | `startedAt >=` | `startedAt` |
| `users/{uid}/outbox` | `status ==` | なし |
| `users/{uid}/assistantTurns` | なし | `createdAt` |
| `users/{uid}/tasks`、`users/{uid}/calendarLinks`、`calendarChannels` | なし | なし |

項目ごとに次を設定している。

- **TTL**：`idempotency`・`outbox`・`oauthStates`・`accountJobs` の `expiresAt`。期限を過ぎたドキュメントを Firestore が自動で消す。
- **インデックスの除外**：`idempotency.body`（記録した応答の本文）。大きな本文がインデックスの上限（1項目 7.5 KiB）に当たって書き込みが失敗しないようにする。

新しい検索を書くときは、同じ項目の条件と並べ替えだけにする。別の項目を組み合わせる検索が必要になったら、
エミュレーターのテストが `FAILED_PRECONDITION: The query requires an index` を返すので、そのときに `terraform/firestore.tf` へ複合インデックス（`google_firestore_index`）を加える。

## 反映のしかた

`deploy/terraform` で `terraform apply` する（[deploy/terraform/README.md](terraform/README.md)）。

ルール（`firestore.rules`）はクライアントからのアクセスを全部拒否する。クライアントは必ず API を通す（AGENTS.md「システム全体」）。

## エミュレーターでの確認

リポジトリにエミュレーターは入れない。Java を用意して、`pnpm dlx` で Firebase CLI を呼ぶ。

```bash
pnpm dlx firebase-tools@latest emulators:exec --only firestore --project demo-dopagaki \
  "bash -c 'cd apps/api && go test -race -count=1 ./...'"
```

`FIRESTORE_EMULATOR_HOST` があるときだけ、`internal/adapter/firestore` のテスト（バックエンドの約束と、API を通した一連の操作）が動く。
