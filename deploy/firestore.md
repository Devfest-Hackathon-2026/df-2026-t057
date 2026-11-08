# Firestore のインデックスとルール

`firestore.indexes.json` と `firestore.rules` が正。Terraform へ移すまでは、ここに書いたコマンドで反映する（AGENTS.md「後回しにしていること」）。

## 何を定義しているか

API の Firestore の検索は、すべて1つの項目の条件と並べ替えだけで書いている（`platform/store` の `Query` の約束）。
そのため **複合インデックスは不要**で、`indexes` は空。検索の一覧:

| コレクション | 条件 | 並べ替え |
|---|---|---|
| `users/{uid}/ledger` | `seq <`（ページ送り）、`at >=`（集計） | `seq`、`at` |
| `users/{uid}/viewingSessions` | `startedAt >=` | `startedAt` |
| `users/{uid}/outbox` | `status ==` | なし |
| `users/{uid}/assistantTurns` | なし | `createdAt` |
| `users/{uid}/tasks`、`users/{uid}/calendarLinks`、`calendarChannels` | なし | なし |

`fieldOverrides` で次を設定している。

- **TTL**：`idempotency`・`outbox`・`oauthStates` の `expiresAt`。期限を過ぎたドキュメントを Firestore が自動で消す。
- **インデックスの除外**：`idempotency.body`（記録した応答の本文）。大きな本文がインデックスの上限（1項目 7.5 KiB）に当たって書き込みが失敗しないようにする。

新しい検索を書くときは、同じ項目の条件と並べ替えだけにする。別の項目を組み合わせる検索が必要になったら、
エミュレーターのテストが `FAILED_PRECONDITION: The query requires an index` を返すので、そのときにここへ複合インデックスを加える。

## 反映のしかた

```bash
# Firebase CLI（推奨。firebase.json の firestore.rules / firestore.indexes にこのディレクトリのファイルを指定する）
firebase deploy --only firestore --project "$PROJECT_ID"

# gcloud だけで反映する場合（TTL とインデックスの除外）
gcloud firestore fields ttls update expiresAt --collection-group=idempotency --enable-ttl --project "$PROJECT_ID"
gcloud firestore fields ttls update expiresAt --collection-group=outbox --enable-ttl --project "$PROJECT_ID"
gcloud firestore fields ttls update expiresAt --collection-group=oauthStates --enable-ttl --project "$PROJECT_ID"
gcloud firestore indexes fields update body --collection-group=idempotency --disable-indexes --project "$PROJECT_ID"
# 複合インデックスを加えたとき
gcloud firestore indexes composite create --collection-group=<group> --field-config=field-path=<a>,order=ascending --field-config=field-path=<b>,order=descending --project "$PROJECT_ID"
```

ルール（`firestore.rules`）はクライアントからのアクセスを全部拒否する。クライアントは必ず API を通す（AGENTS.md「システム全体」）。

## エミュレーターでの確認

リポジトリにエミュレーターは入れない。Java を用意して、`pnpm dlx` で Firebase CLI を呼ぶ。

```bash
pnpm dlx firebase-tools@latest emulators:exec --only firestore --project demo-dopagaki \
  "bash -c 'cd apps/api && go test -race -count=1 ./...'"
```

`FIRESTORE_EMULATOR_HOST` があるときだけ、`internal/adapter/firestore` のテスト（バックエンドの約束と、API を通した一連の操作）が動く。
