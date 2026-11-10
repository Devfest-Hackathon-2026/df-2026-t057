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

## 未確認

- `calendar.events.owned.readonly` が Google の審査で「機密」に当たるか。OAuth の同意画面の設定（スコープの宣言）にこのスコープを足す作業と合わせて、Google Cloud コンソールで確かめる。
- 実際の Google アカウントで、主カレンダーの予定が表示されること。
