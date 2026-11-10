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

## 2026-11-10の本番反映

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

README更新を保持し、[再配信用PR #73](https://github.com/a-sumikawa/devfest-2026/pull/73)を `[deploy]` 付きでmain `4c1e1458bc8601217f49697ff981fa158f000753` へマージした。[再配信のCI/CD](https://github.com/a-sumikawa/devfest-2026/actions/runs/42765319104)は、生成物・API・frontend・Terraform検証・本番イメージのスモークE2E・総合check・配信がすべて成功した。配信ガードの変更や回避はしていない。

2026-11-10 15:03（日本時間）に、サービスと実際に配信中のリビジョン自身を読み取って照合した。

- 配信リビジョン：`dopagaki-00073-bef`、100%。
- 配信ソース：main `4c1e1458bc8601217f49697ff981fa158f000753`（主カレンダー表示を取り込んだコミットは `e36df6d471f9c09869f90a29f90c487c00408f46`）。
- イメージdigest：`sha256:6548089e4945ed155b0a582e018d027176191b0d1fa38677828fdbdf9aa6e8ed`。Artifact Registryのmainコミットタグと配信中のリビジョンで一致した。
- AI上限：1人10回/分・500回/日、全体60回/分・2000回/日を維持した。
- Firestoreの時点復旧：`POINT_IN_TIME_RECOVERY_ENABLED` を維持した。

Cloud Runの公開URLと、設定上の `APP_URL` の両方で、以下を確認した。認証情報を付けず、個人の予定にはアクセスしていない。

| 確認先 | 応答 |
|---|---|
| `GET /api/health` | 200、`status: ok` |
| 未認証 `GET /api/profile` | 401 |
| 未認証 `GET /api/calendar/events?from=...&to=...` | 401 |

[秘密値を含まない照合結果](evidence/calendar-primary-rollout-2026-11-10.json)に、リビジョン・コミット・digest・上限・HTTP応答と確認時刻を保存した。実際のGoogleアカウントでの再同意と、同意後の主カレンダー表示は未確認のまま区別する。

### mainとdevelopの同期

本番の確認結果もmainへ記録してから、developをmainの先端へfast-forwardする。forceは使わず、更新前のdevelopのSHAを指定して、並行した変更を上書きしない。同期後のSHAと差分確認結果はPR #72へ残す。

Terraformの変更・適用、Googleへの審査申請、個人カレンダーへの書き込み、Geminiの有料呼び出しは今回行っていない。既存のAI上限と、Firestoreの時点復旧の設定も本番反映時に照合した。
