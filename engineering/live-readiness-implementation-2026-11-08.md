# 実利用に向けた6項目の実装

2026-11-08、日本時間。対象はブランチ`codex/live-readiness`、基準は`origin/main`の`ade57da`。ローカル実装の記録であり、本番配信・公開審査・実ユーザーでの動作確認の記録ではない。

## 範囲

ユーザー指定の`Downloads/AGENTS.md`をリポジトリへ配置し、`CLAUDE.md`から同じファイルを参照するリンクを作成した。後続の指定「全面的な作り直しは別でしているので、6項目に必要な変更まで」に従い、現行の`api/`・`web/`・`extension/`構成を使う。

対象は①接続維持、③同期復旧、④導入確認、⑤履歴を残す整理、⑥停止・書き出し・削除、⑦振り返り。②の時間割・FreeBusy・中断操作の追加は行わない。既存の睡眠、相棒、視聴ルール、通知モード、AI相談・記憶を残す。

準備案の基準だった古い作業ブランチには最新機能がなかったため、最新mainを専用worktreeへ取り出して実装した。報酬はmainの「作業5分で視聴1分」、初回10分、各上限等を維持する。準備案の旧報酬へ戻さない。元の作業領域の未コミット変更は取り込んだり破棄したりしていない。

添付文書にあるpnpm/WXT、全体のパッケージ分割、OpenAPI生成クライアント、SSE、Cloud Tasks/Outbox、台帳全体の再設計は別の全面改修側の作業とする。今回の新しい記録の集計はGoの`internal/records`に置き、既存のHTTP・保存アダプターに接続した。本番データの移行・リセットは実行していない。

## 変更

| 対象 | 追加した動作 | 主なファイル |
|---|---|---|
| ① | 拡張がFirebase SDKの永続化で本人の認証を保持。呼び出しごとにSDKからIDトークンを取得。Webと別アカウントなら自動切替せず案内 | `extension/src/auth.ts`、`background.ts`、`offscreen.ts`、`web/auth/extension-auth.ts` |
| ③ | 同時編集を課題別に表示。アプリ・Google・手編集の候補を見て確定。プレビュー時の版・ETagと再試行IDを保持。再競合時は比較内容を更新 | `calendar_recovery.go`、`calendar.go`、`calendar-conflicts.ts` |
| ③ | 一時失敗と再認証待ちを区別。保留の再同期API、1人の失敗で他の利用者の監視更新を止めない処理 | `calendar.go`、`maintenance.go` |
| ④ | `/setup`でログイン・Calendar・拡張・本人・通知権限を確認。通知の実際の視認は本人が確認。拡張のメニューからテスト通知 | `setup-status.ts`、`popup.ts`、`extension-help.html` |
| ⑤ | 完了記録と整理した課題を別コレクションへ保存。課題を整理しても草・記録を保持。再送と再取り込みで整理済み課題を再作成しない | `records.go`、`store.go`、`tasks.*`、`records.*` |
| ⑤ | 通常の記録読み込みを直近12週間へ限定。一覧を100件ずつ取得し、集計はページの件数に依存させない | `internal/records/domain.go`、`records.go`、`records.ts` |
| ⑥ | 本人のJSON/CSV書き出し、会話全削除と任意の記憶削除、Calendar連携停止、退会受付と再試行、削除状況の確認 | `account.go`、`account_export.go`、`account-tools.ts`、`deletion-status.*` |
| ⑦ | 開始時の予定、確定スヌーズ、停止・再開、確認した作業を保存。プロフィールのタイムゾーンと月曜始まりで集計 | `records.go`、`internal/records/domain.go` |
| ⑦ | 一時停止を明示的な状態設定にし、同じPUTの再送で再開しない。旧POSTは互換用に維持 | `account_export.go`、Web/拡張のタイマー操作 |

設定に加えた操作はMaterialの開閉パネルへ入れ、既存のコンパクト一覧を維持する。空のエラー要素は表示しない。

### 認証と埋め込み

本番のWeb→拡張メッセージではBearerを転送しない。デモ接続は従来どおり。Webのログアウトは該当UIDだけを拡張から解除し、別アカウントの拡張を解除しない。オフライン起動時に設定取得が失敗しても次回に再試行する。

GoogleログインはFirebase公式のOffscreen方式を用いる。認証専用ページだけ、許可した拡張IDのiframeを認める。通常画面の`X-Frame-Options: DENY`・`frame-ancestors 'none'`は残す。認証結果の受信はorigin・sourceと拡張内の送信ページを確認する。

許可した拡張ID以外からの状態変更リクエストを拒否する。本番では`EXTENSION_IDS`が必要。デモはパッケージ化していない拡張の試験を許可する。

### 保存と既存データ

追加する保存先は以下。クライアントのFirestore直接アクセス拒否は維持する。

- `users/{uid}/completions/{taskId}`：独立した完了日時・課題名・見積もり・得られている実績。
- `users/{uid}/activityEvents/{eventId}`：開始・スヌーズ・停止・再開・作業確認。
- `users/{uid}/archivedTasks/{taskId}`：整理した完了課題。
- `accountJobs/{uid}`：退会の受付、再試行、完了確認用トークンのハッシュ、完了後の期限。

操作の保存と記録追加は同一Firestoreトランザクションで行う。同じ記録IDがあれば重複追加しない。通常の視聴消費等の保存で、過去の完了記録を全件読み直して追加判定しない。

既存の課題・台帳から確認できる完了・開始・作業実績は表示に利用する。以前のスヌーズ等、保存されていない行動は補完しない。旧課題を最初の操作で整理しても完了記録を保存する。残高を再計算したり、登録・完了APIを呼んで履歴を移したりしない。

今回、Stateと台帳の全保存形式は変更しない。ユーザー全体のデータモデル・台帳の分離と移行方針は、別の全面改修に属する。今回の記録・整理は既存データ上で動作し、大きなリセットを前提にしない。

### Calendarの解除と復旧

通常の連携解除では同期と監視を停止し、ユーザー専用Secret全体を削除する。Google側のプロジェクト全体のOAuth認可は取り消さない。再接続用に専用Calendarの対応を残す。古い監視で`resourceId`がない場合、Google側の監視停止は期限まで残り得るが、アプリ側の同期は拒否する。

再認証待ちと編集競合は自動上書きしない。保守の同期再試行は保留状態が対象。外部の編集がさらに進んだら、確認時のETagで拒否して新しい比較を取得する。

Google側の削除、進行中の課題、期限切れ等で既存の編集検証を満たせない場合は確定を拒否する。あらゆるGoogle削除・認可状態を自動修復できるという意味ではない。実Googleでの複合故障・再認可の確認は公開前に行う。

### 削除の完了条件

本番は最近の再認証後に受付し、以降の書き込みを遮断する。通常APIの処理期限を50秒にし、既存の処理を待つため受付後90秒以降に保守処理する。AI会話の削除中は新しい相談の保存を止め、以前の応答で履歴を復活させない。

保守処理は監視・専用トークン、会話、記録・整理済み課題、本人のOAuth状態・AI利用カウンター、状態、Firebaseユーザーを順に削除する。専用Calendarの削除は本人の選択による。失敗した受付は残し、別の利用者の処理も進める。

受付トークンはブラウザーのセッション保存領域に置き、サーバーにはハッシュだけを保存する。本人のGoogleログアウト後も、トークンで進行状態だけを確認できる。本文やUIDは状況APIに含めない。完了した受付情報だけに7日後の`expiresAt`を設定する。未完了の受付はTTLで消さない。

### 主なAPIの追加

| API | 用途 |
|---|---|
| `GET /api/records?cursor=...` | 直近12週間の集計、100件単位の完了記録 |
| `POST /api/tasks/{id}/archive` | 記録を残して完了課題を整理 |
| `GET /api/tasks/archived` | 本人の整理した課題 |
| `PUT /api/session/pause` | `{paused: true/false}`による明示操作 |
| `POST /api/tasks/{id}/calendar-resolution?preview=true` | 候補の検証・プレビュー |
| `POST /api/tasks/{id}/calendar-resolution` | 確認時の版・ETag・IDで確定 |
| `POST /api/calendar/disconnect` | Calendar連携解除 |
| `GET /api/account/export?format=json/csv` | 本人情報の書き出し。トークン・再送記録は除外 |
| `POST /api/assistant/delete-history` | 会話全削除。`deleteMemory`は別選択 |
| `POST /api/account/deletion` | 本人の再認証後に削除受付 |
| `GET /api/account/deletion-status` | `Authorization: Deletion <受付トークン>`で進行状態のみ取得 |
| `POST /api/calendar/retry`・`POST /api/account/cleanup` | 許可したScheduler SAのOIDC、または既存の保守用Secretによる保守 |

## 配信前の設定と確認

1. `EXTENSION_IDS`に公開・開発の必要な拡張IDを設定する。Firebaseの許可設定と認証ページのiframe・Googleログインを実Chromeで確認する。
2. 実行SAにFirebaseのユーザー確認・削除と、ユーザー専用Calendar Secretの削除に必要な最小権限を確認する。共有のOAuth client secret・Scheduler secretの削除を許可しない。
3. 専用Scheduler SAを`MAINTENANCE_EMAIL`に設定し、[保守設定スクリプト](../deploy/setup-readiness-maintenance.sh)で再同期・削除のジョブと完了情報のTTLを設定する。スクリプトは**未実行**。初回作成用で、既存ジョブの変更は`update`を使う。ジョブを呼ぶSAには本人データの操作権限を付与しない。
4. 未認証・別SAの拒否、正しいOIDCの受信、実Googleでの同時編集・再認可・解除、専用Secret削除、Firebaseユーザー削除を架空の別ユーザーで確認する。
5. 以上が済んでから`ACCOUNT_CLEANUP_ENABLED=true`にする。既定値はfalseで、Calendar設定がない場合も無効。退会が有効でない環境は運営への削除依頼を使う。
6. 拡張を公開オリジン向けにビルドし、既存の拡張を更新する。Chrome Web Store申請・名義・掲載資料は別途必要。今回、申請や公開はしていない。

設定の例は`.env.example`と`deploy/cloudrun.env.example.yaml`に追記した。既存の秘密値の取得・表示・変更は行っていない。Cloud Run、IAM、OAuth、Scheduler、TTL、本番データ、公開拡張は変更していない。

## ローカル検証

Node 24.22.1、Go 1.27.3、Java 21のローカルFirestore Emulatorを使用。Web/拡張の依存は既存インストールを使用し、新しい製品依存は追加していない。

```bash
npm run typecheck
npm run test:unit
npm run build
(cd api && go test -race ./... && go vet ./...)
DOPAGAKI_EMULATOR_TEST=1 FIRESTORE_EMULATOR_HOST=127.0.0.1:8686 \
  sh -c 'cd api && go test -race ./...'
APP_URL=http://127.0.0.1:4259 npm run build:extension
E2E_PORT=4259 npm run test:e2e -- --workers=2
```

- Node単体18件：アカウント切替、拡張のオフライン起動からの再試行、SDKトークンの取得、配布先・版の固定、既存の時刻・報酬表示等。
- Go race・vet：既存全件と追加の記録保持、削除後の書き込み拒否、CSV、ページ境界、所有者分離、認証ページのCSPと拡張IDの制限。
- Emulator：別クライアントのAI上限に加え、記録の原子的保存・整理・残高維持、削除受付後と削除後の書き込み拒否、受付トークンでの状態取得。
- E2E：既存画面と拡張を含む33件を対象。新規に整理後の記録、導入診断、削除キャンセルと書き出し、競合プレビューの版・ID保持を追加。

制限付き実行でAngularが異常終了し、ローカルHTTPのlisten/接続も拒否されたため、同じビルド・試験を制限外で実行した。途中のE2Eは空のalert要素・見出しの曖昧な検索・既存表示文言との不一致で失敗し、修正した対象を再実行した。

実Googleログイン、管理画面を閉じた1時間以上の接続、実Chrome再起動・スリープ、実Google競合・Secret/Firebase削除、保守OIDC、ストア配布は未確認。有料Vertex AIと実Calendarを通常テストで呼んでいない。ローカル成功を本番導入済みとは扱わない。

## 公式資料

- [FirebaseのChrome拡張認証](https://firebase.google.com/docs/auth/web/chrome-extension)
- [IDトークンの失効確認](https://firebase.google.com/docs/auth/admin/manage-sessions)
- [Calendarの監視停止](https://developers.google.com/workspace/calendar/api/v3/reference/channels/stop)
- [OAuth認可の取り消しが及ぼす範囲](https://developers.google.com/identity/protocols/oauth2/web-server#tokenrevoke)
- [Firestoreの子コレクション削除](https://firebase.google.com/docs/firestore/manage-data/delete-data)
- [FirestoreのTTL設定](https://docs.cloud.google.com/sdk/gcloud/reference/firestore/fields/ttls/update)

### 最終結果

- TypeScript型チェック、Web/拡張のビルド：成功。
- Node単体：18件成功。
- Go：全パッケージのrace試験（Emulator試験を含む）、vetが成功。削除後に遅延したAI保存が復活しない試験も成功。
- E2E：全33件の実行では32件成功、残り1件は既存の開始件数の表示文言との不一致。修正後、当該試験を含む関連10件がすべて成功した（41.1秒）。全体と対象再実行を合わせて33件の確認を完了。
- `git diff --check`、保守スクリプトの`bash -n`：成功。
- [検証範囲の機械可読記録](evidence/live-readiness-validation-2026-11-08.json)。実環境の検証項目は未実施のまま区別した。

変更は専用ブランチでコミット・PRとして共有する。本番配信は未実施。別で進行中の全面改修へは、この変更を必要な機能ごとに取り込める。最初の作業領域の既存差分は維持した。

### PR共有時点のmainとの差分

コミット・PR作成の依頼後、リモートmainへ全面改修PR #29（`269efda`）がマージされたことを確認した。今回の実装はその前の`api/`・`web/`・`extension/`構成を基準にしている。旧構成のファイルを再追加する形で全面改修へ重ねないため、試みたリベースは中止した。

6項目の成果は旧基準で検証したコミットとしてDraft PRで共有する。現行mainとの競合は未解消で、新構成への移植後に必須チェックの再実行が必要。上記のローカル成功を現行mainへの適用・検証成功とは扱わない。mainの巻き戻しや全面改修の上書き、本番配信は行っていない。
