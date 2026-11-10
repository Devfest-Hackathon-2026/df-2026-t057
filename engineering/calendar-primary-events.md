# Googleカレンダーの予定の表示

記録日：2026-11-09。ブランチ `feat/login-google-connections`。ローカルでの実装と自動テストまで。本番への反映と、実際の Google アカウントでの確認はまだ行っていない。

## 背景

連携しても、もともと Google カレンダーにある予定が DOPAGAKI に出なかった。不具合ではなく、スコープが `calendar.app.created` だけで、DOPAGAKI が作った専用カレンダーしか読めない作りだったため。ユーザーの指示で、カレンダー画面に Google の予定を重ねて表示し、チェックボックスで DOPAGAKI の予定と Google の予定をそれぞれ表示・非表示できるようにした。

## 決めたこと

- 読むのは主カレンダー（`primary`）だけ。カレンダーの一覧と選ぶ画面を作らずに済む。
- スコープは `calendar.events.owned.readonly`。公式の説明は「See the events on Google calendars you own」で、`events.list` で使えることを [Events: list](https://developers.google.com/workspace/calendar/api/v3/reference/events/list) の認可の表で確認した。すべてのカレンダーを読める `calendar.events.readonly` より狭い。
- 予定は `GET /api/calendar/events?from=&to=` で、表示している期間（42日まで）だけをその場で Google から読む。Firestore に保存せず、課題に取り込まず、AI に送らない。副作用がないので Outbox を通さない。
- 同意画面で本人がこのスコープを外すことがあるので、トークンの応答の `scope` を見て `users/{uid}/state/calendar` の `primaryEvents` に記録する。`CalendarStatus.primaryEvents` が false なら、画面は Google の予定のチェックボックスを無効にし、もう一度連携するよう案内する。この機能より前に連携した人も false になる。
- チェックボックスの状態は表示の好みなので、ブラウザーの localStorage（`dopagaki.calendarLayers`）に覚える。読めなければ両方とも表示する。
- 終日の予定があるときだけ、FullCalendar の終日の欄を出す。

## 見送った案

- 予定を課題として取り込む案：既存の取り込み（専用カレンダー）と二重になり、報酬の付与も絡むため。
- 変更通知（watch）で主カレンダーを監視する案：表示だけなら、期間を移したときと「Googleと同期」を押したときに読み直せば足りる。

## 初期実装時の未確認（2026-11-09）

- `calendar.events.owned.readonly` が Google の審査で「機密」に当たるか。OAuth の同意画面の設定（スコープの宣言）にこのスコープを足す作業と合わせて、Google Cloud コンソールで確かめる。
- 実際の Google アカウントで、主カレンダーの予定が表示されること。

## 2026-11-10の本番反映準備

[PR #72](https://github.com/a-sumikawa/devfest-2026/pull/72)で、developへマージ済みのPR #54をmain `e36df6d471f9c09869f90a29f90c487c00408f46` へ取り込んだ。mainにあったAI上限の本番反映記録も保持している。

### OAuthの申告

本人の明示承認後、Google Auth Platform（`devfest-2026-513816`）のデータアクセスへ `https://www.googleapis.com/auth/calendar.events.owned.readonly` を追加し、保存完了通知と保存後の一覧を確認した。既存の `calendar.app.created`・`userinfo.email`・`openid` は維持した。

このスコープはコンソールで「機密性の高いスコープ」「まだ検証されていません」と表示された。公開ステータスは「本番環境」、ユーザーの種類は「外部」のまま。未確認の機密スコープに対するOAuthユーザー数の上限は100人と表示され、確認時点の利用者数は0人だった。

申告の保存は、Googleによる審査完了や、各利用者による読み取り同意ではない。審査申請は実施していない。既存の連携では `primaryEvents=false` なので、利用者本人が再連携し、追加の読み取りを許可する必要がある。申告するスコープ自体は本人が所有するカレンダーの予定を読む権限で、DOPAGAKIのコードは `primary` だけを表示用に読む。

### 自動・模擬確認

- [main向けPRのCI](https://github.com/a-sumikawa/devfest-2026/actions/runs/42763234844)：API・frontend・スモークE2E・総合checkが成功。Terraform構成の変更はない。
- Node 24.22.1で新しい管理画面の本番ビルドが成功。
- 架空データを使った表示確認2件が成功。時刻付き・終日のGoogle予定の表示、DOPAGAKI／Googleの個別表示切替、再読み込み後の選択保持、390pxでの横はみ出しなし、ライト／ダークのaxe違反なしを確認した。
- 新しいscopeに未同意の場合は、Googleのチェックボックスが無効で再連携の案内が出て、予定取得を行わないことを確認した。

表示の追加確認は、新しいWebのビルドと既存デモAPIを組み合わせ、Google予定のAPI応答をモックして行った。新しいGoogle APIのバックエンド処理はCIのGoテストで別途検証した。実際のGoogleアカウントでの再同意と予定表示は、この確認には含めない。

### 本番の配信と照合

mainの[CI](https://github.com/a-sumikawa/devfest-2026/actions/runs/42763948708)はAPI・frontend・本番イメージのスモークE2E・総合checkがすべて成功した。ただし実行中にREADMEのみの更新 `e3245f8a4131966a36d20f35c733d361944357fd` がmainへ入ったため、最新mainだけを配信するガードが働き、配信ジョブはスキップされた。コードの検査失敗ではない。

README更新を保持し、承認済みの本番反映依頼に従って、再配信用PRのmainへのマージコミットに `[deploy]` を付けて最新mainからCI/CDを実行する。配信後のリビジョン・digest・HTTP応答の結果は確認後に追記する。現時点の稼働リビジョンは `dopagaki-00071-das`。

### mainとdevelopの同期

本番の確認結果もmainへ記録してから、developをmainの先端へfast-forwardする。forceは使わず、更新前のdevelopのSHAを指定して、並行した変更を上書きしない。同期後のSHAと差分確認結果はPR #72へ残す。

Terraformの変更・適用、Googleへの審査申請、個人カレンダーへの書き込み、Geminiの有料呼び出しは今回行っていない。既存のAI上限と、Firestoreの時点復旧の設定は本番反映時にも照合する。
