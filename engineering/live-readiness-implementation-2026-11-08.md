# 実利用に向けた6項目の実装

2026-11-08、日本時間。PR #30 / `codex/live-readiness`。

## 今回の基準

mainの全面改修PR #29（`269efda`）を取り込み、旧`api/`・`web/`・`extension/`向けの変更を`apps/`と`packages/`へ移植した。旧構成のファイルは復活させていない。②のスケジューリング仕様は変更していない。本番配信、クラウド設定、本番データのリセットは行っていない。

最初の実装コミット`d4624b9`は旧基準の履歴として残す。現在の実装と検証については、この文書と[検証記録](evidence/live-readiness-validation-2026-11-08.json)を参照する。[準備記録](live-readiness-preparation-2026-11-08.md)は着手前の設計案で、旧パスや不採用案を含む。

## 移植した機能

| 項目 | 現在の実装 | 主な場所 |
|---|---|---|
| ① 拡張の継続接続 | 拡張自身がFirebase SDKのIndexedDB永続化とトークン更新を利用。WebからはアカウントIDだけを渡す。Googleログインはoffscreenと認証専用ページを使用。アカウント切替では状態を消し、遅れた通信を捨てる | `apps/extension/background/{auth,connection,router}.ts`、`entrypoints/offscreen/`、`apps/web/src/app/features/onboarding/extension-auth.ts` |
| ③ Google同期の復旧 | 既存のOutbox・Cloud Tasksを利用。失敗ジョブの再試行、再認可待ちの区別、両側編集の競合一覧と採用操作を追加。比較したGoogle ETagと課題の版を確認し、処理中の再編集は再確認へ戻す | `apps/api/internal/calendar/recovery.go`、`platform/outbox/`、Webの`features/calendar/calendar-conflicts.ts` |
| ④ 初回導入 | ログイン・Calendar・拡張の検出と、同一アカウント・通知権限の診断を区別。通知はポップアップからテストし、本人の視認は別操作で確認する | Webの`features/onboarding/setup-status.ts`、`packages/bridge/`、拡張のpopup/management |
| ⑤ 長期利用 | 完了記録を課題とは別に保存。完了課題をアーカイブ・復元でき、アーカイブ後や課題削除後も完了集計と台帳を残す。取り込みでアーカイブ済みIDを復活させない。一覧は100件ずつ返す | `apps/api/internal/task/archive.go`、`activity/`、`records/`、Webの`features/tasks/archives.ts` |
| ⑥ 利用停止・削除 | JSON/CSV書き出し、会話と任意の記憶の削除、Calendarの連携解除、最近の再認証後の退会を提供。退会後は共通保存層で本人の書き込みを拒否し、90秒の待機後に外部連携・データ・Firebase利用者の順で清掃 | `apps/api/internal/account/`、`platform/store/account.go`、各adapter、Webの`features/settings/account-tools.ts` |
| ⑦ 振り返り | セッション開始・確定したスヌーズ・確定した作業時間を独立記録へ保存。本人のタイムゾーン・月曜始まりで12週分を集計。未発生の過去の行動を生成しない | `activity/`、`worksession/`、`task/`、`records/`、Webの記録画面 |

報酬はmainにある追記型台帳を正とし、今回の機能で別の残高管理を作っていない。APIはOpenAPIを変更してからGo/TSを生成し、既存のProblem Details・Idempotency-Key・If-Matchを使う。

## 認証と削除の境界

- 認証専用の`/extension-auth`だけを、設定した拡張IDからframeに読み込める。通常のページと削除状況ページはframeを拒否する。
- 認証専用ページは通常の管理画面のAuth/bridgeを起動しない。Googleの認証結果だけを親のoffscreenへ返し、その画面のFirebase認証はメモリ内で扱う。
- Webと拡張のREADY/CONNECTを往復し続けないよう、接続応答からREADYを再送する処理を外した。認証確認を待つ間の古いポート通知を破棄し、後から届く初期取得結果で新しい表示を消さない。非同期の順序を再現する回帰試験を追加した。
- Firebase IDトークンは失効・削除も確認する方式へ変更。実環境で必要なFirebase権限は未確認。
- `accountJobs/{uid}`を削除開始の印とし、トランザクションの確定直前にも確認する。要求の再送応答の保存は許可し、退会受付の同じリクエストを再送できる。
- 受付記録には照会用のランダム値のハッシュを持つ。ログアウト後はURLのfragmentで受け取った受付値で`/deletion-status`から照会する。削除完了後だけ7日間のTTLを付ける。
- 清掃は1回最大10人で、失敗した人が先頭を占め続けない順に処理する。Firestoreは未知の子コレクションと入れ子も削除し、途中失敗後の再試行に対応する。Firebase利用者の削除は最後に行う。
- Calendarの通常の連携解除はmainの監視停止・専用Secret削除を利用し、Google側のカレンダーは残す。通常解除時の外部失敗は既存のログ記録方式を維持する。退会清掃では外部失敗を返し、再試行する。
- 書き出しは許可した本人の状態と記録・会話を対象にし、Calendar認証情報・ショートカットキー・保守ジョブ・再送受付を含めない。

## 設定と運用

- `EXTENSION_IDS`：mainと同じカンマ区切り。認証専用ページのframe許可にも使う。
- `ACCOUNT_CLEANUP_ENABLED`：liveの既定は`false`。有効化にはCalendarと保守OIDCの設定が必要。demoでは合成データの退会操作を利用できる。
- `SCHEDULER_INVOKER`、`OIDC_AUDIENCE`、`APP_URL`：既存の保守認証を使う。旧案の`MAINTENANCE_EMAIL`は追加しない。
- `deploy/setup-readiness-maintenance.sh`：毎分の清掃ジョブと`accountJobs.expiresAt`のTTLを用意する。**未実行**。
- 実環境ではSchedulerのOIDC、Firebaseの失効確認・利用者削除、Secretの削除権限と、旧リビジョンの処理が終了したことを確認してから退会清掃を有効化する。
- mainへのマージは自動配信を起動するため、今回の作業でマージはしない。

## 検証

Node.js 24.22.1、pnpm 12.15.0、Go 1.27.3、golangci-lint 2.14.0で実行。Goの外部HTTPはモック、FirestoreはローカルEmulatorの合成データを使用。

実行コマンドと最終結果は[検証記録](evidence/live-readiness-validation-2026-11-08.json)に記載する。退会後の別トランザクションからの書き込み拒否、100件超と入れ子の清掃、アーカイブ後の履歴・報酬、週境界とDST、競合比較後の再編集、退会応答の再送を回帰確認に含める。

E2Eで退会受付の再送が403になる不具合を再現した。生成された埋め込みOpenAPIのoperationIdがGo名（`DeleteAccount`）へ正規化される一方、middlewareは契約名（`deleteAccount`）で照合していたのが原因。ルート読込時に表記を揃え、HTTP回帰テストを追加した。

実Google OAuth、1時間以上の認証更新、実Chromeの再起動・スリープ、実Googleの競合・再接続、実Secret/Firebaseの削除、GCP上の保守OIDC、ストア配布は未確認。有料Vertex AIと実Calendarは通常テストから呼んでいない。ローカル成功を本番導入済みとは扱わない。

### 最終結果

- `pnpm generate`：再生成前後のGo・TS・トークン生成物のハッシュが一致。
- `pnpm check`：整形、TS/CSS/Go lint、OpenAPI、コントラスト、型チェックが成功。
- 単体：Web 127件、拡張132件、bridge 2件、messages 11件、配信処理のモック6件が成功。
- 全Goパッケージのrace試験：Emulatorを含め成功。
- Web・WXT拡張・Go APIのビルド：成功。
- E2E：最終の全58件が成功（2.2分）。途中で見つかった退会再送と拡張の非同期順序の不具合は、回帰試験を追加して修正した。
- 保守スクリプトの`bash -n`、`git diff --check`：成功。

本番配信・マージ・クラウド設定は未実施。実環境の未確認項目は残したまま区別している。
