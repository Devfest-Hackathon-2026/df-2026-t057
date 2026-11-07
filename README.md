# DOPAGAKI

YouTube Shortsから、いま始める課題へ戻るためのChrome拡張と管理画面です。締切から逆算した作業予定を通知し、完了した分だけ視聴できる時間が増えます。

公開環境：[DOPAGAKI](https://dopagaki-1032251723022.asia-northeast1.run.app/)。Googleログインして使います。Chrome拡張は、この公開先を指定してビルドしてください。

課題・予定・視聴時間はGoogleアカウントごとに保存します。カレンダー連携もログイン中と同じGoogleアカウントを使い、本人の専用DOPAGAKIカレンダーへ登録します。管理画面の「AIに相談」で会話すると、編集できる予定カードや作業傾向の記憶候補が表示されます。登録・変更・記憶は本人が確認してから確定します。Google Chat連携は廃止しました。[一般ユーザー対応の現状](engineering/public-accounts.md)を参照してください。

実装の仕様は、この会話の確定事項と[指定のSlack投稿]([内部リンク削除])を基にしています。`docs/` は別案の資料であり、このアプリの仕様ではありません。

## 開発記録と発表用の根拠

[engineering/README.md](engineering/README.md) に、設計判断、不具合の症状と原因、運用・CI/CDの現状、実サービス検証、発表用の論点と実画面をまとめています。今後の変更でも、関連する判断・知見・検証結果を併せて更新します。Terraformは未導入、CDによる自動デプロイは未整備であり、CIの成功や手動配信とは区別しています。

## ローカルで試す

Node.js 24.15以上、Go 1.26以上が必要です。

```bash
npm ci
npm run dev
```

[管理画面](http://127.0.0.1:4200)を開きます。ローカルデモではGoogle認証や有料APIを使いません。課題のない状態から登録するか、「サンプル3件で試す」を押せます。デモのデータはAPIのメモリー内にあり、APIを停止すると消えます。

Chrome拡張は次のコマンドで作成します。

```bash
npm run build:extension
```

`chrome://extensions` でデベロッパーモードを有効にし、「パッケージ化されていない拡張機能を読み込む」から `dist/extension` を指定します。その後、管理画面を開き直すと同じユーザーのデータに接続されます。

拡張のポップアップにある「この作業サイトにタイマーを表示」を押すと、そのサイトだけにタイマーを表示する権限を求めます。最初からすべてのサイトへのアクセスは要求しません。

## 管理画面の構成

ホームは日・週のカレンダーです。予定を選ぶと右側に課題カードが開き、「この予定について相談」から対象を付けてAIに相談できます。左メニューはカレンダー・課題・記録、設定とGoogle連携は右上です。狭い画面では予定と相談を切り替え、未送信の文は残します。画面と確認範囲は[開発記録](engineering/calendar-workspace.md)を参照してください。

## 動作

- 視聴時間は初回60分。新しい課題を登録すると10分、完了すると20分加算。繰り越し可能。再送や二重クリックでは増えません。
- 「課題を登録」では「AIに相談／手入力」を選べます。未開始の課題は、課題一覧の「内容を調整・AIに相談」またはカレンダー詳細の「課題の内容を調整」から、変更前後と時間割を確認して編集できます。編集では視聴時間を付与しません。
- 視聴時間は、前面で再生しているYouTube Shortsだけを計測します。通常動画・バックグラウンド・一時停止中は対象外です。
- 通知の時刻は、課題の締切と必要時間から逆算した開始時刻です。通常はChrome通知、Shortsの画面では動画を一時停止して画面全体に表示します。
- スヌーズは初期値10分。候補は通知前に作成し、変更前後の予定を比較できます。時間・順序を手動で変えるか、Geminiに指示し、「OK」で確定します。キャンセルでは変更しません。
- 締切に収まらない場合は、作業時間を短縮した案であることを明示します。1分も確保できない課題や、重複・締切超過は確定できません。
- 課題の開始でタイマーを起動。上端の細いプログレスバー、一時停止・再開、完了ボタンを表示します。タイマーが終わっただけでは報酬を与えません。
- パチンコ・麻雀・シンプルの通知モード、音、動きを控える設定、スヌーズ時間を変更できます。完了履歴は草のカレンダーに反映されます。

Chromeが終了・スリープ中の場合は通知できません。Chrome Alarmsによる通知にはOSやブラウザーの状態に応じた遅延があります。拡張を無効化できるため、強制的な利用制限や不正防止を保証する仕組みではありません。

## 構成

| 部分 | 技術・役割 |
|---|---|
| 管理画面 | Angular 22、Angular Material / CDK、Material Symbols Rounded |
| Chrome拡張 | Manifest V3、TypeScript、Chrome Alarms / Notifications / Storage |
| API | Go、標準 `net/http`、Google公式SDK |
| AI | Vertex AI / Gemini 3.8 Flash。課題を具体的な手順へ分解し、作業傾向を反映 |
| 認証・永続化 | Firebase Authentication（Googleログイン）、Firestoreのトランザクション |
| Google連携 | Google Calendar API。OAuthの更新トークンはSecret Managerに保存 |
| 配信 | Cloud Runが管理画面とAPIを同じオリジンで配信 |

```mermaid
flowchart TD
  G[GeminiのGoogle Calendar連携] --> C[専用DOPAGAKIカレンダー]
  M[Googleカレンダーへ手入力] --> C
  H[管理画面でAIに相談] --> VAI[Vertex AI: 会話と本人情報から提案]
  VAI --> Card[編集できる予定カード / 記憶候補]
  Card --> Confirm{本人が確認}
  Confirm -->|予定を登録| A[Go API: 認証・時刻・版の検証]
  Confirm -->|記憶する| Memory[Firestore: 本人の作業傾向]
  Memory --> VAI
  W[Angular管理画面へ手入力] --> A
  A --> C
  C -->|変更通知 / 差分同期| S[Go API: 課題と本人設定を取得]
  S --> P[Vertex AI / Gemini: 具体的な手順を作成]
  P --> B[Go: 締切から逆算・時間を検証]
  B --> F[Firestoreへ保存 / 新規登録で10分付与]
  F --> D[確定した予定をカレンダー詳細欄へ反映]
  F --> R[スヌーズ候補を事前作成]
  R --> E[Chrome拡張: 開始時刻を予約]
  E --> N{Shorts視聴中?}
  N -->|はい| O[全画面通知]
  N -->|いいえ| T[Chrome通知]
  O --> X{操作}
  T --> X
  X -->|スヌーズ / 調整| V[変更前後を確認・Geminiか手動で修正]
  V --> K{OK?}
  K -->|はい| Q[予定の版・重複・締切を検証]
  Q -->|有効| F
  K -->|キャンセル| R
  X -->|開始| I[タイマー / 上部の細いプログレスバー]
  I --> J[本人が完了を押す]
  J --> L[Firestoreで一度だけ20分付与 / 継続記録を更新]
```

新規課題として初めて登録された場合だけ10分を付与します。予定の変更・同期・スヌーズでは加算しません。作業の傾向や課題のメモは予定作成のためGeminiへ送信します。閲覧中の動画内容や閲覧履歴は送信しません。

カレンダーの利用対象は、連携時に作成する専用の `DOPAGAKI` カレンダーです。他のカレンダーの予定は変更しません。手入力は「開始＝締切−必要時間、終了＝締切」の日時付き予定で登録します。終日・繰り返し予定は対象外です。Geminiでは「DOPAGAKIカレンダーに」と明示します（[Googleの案内](https://support.google.com/gemini/answer/15305236)）。既存メモは保持し、`[DOPAGAKI]` ブロック内に確定予定を記載します。

## Google Cloudで動かすための設定

AIは初期値で1人5回/分・30回/日、全体30回/分・300回/日に制限します。失敗も回数に含み、日次はUTC 00:00（日本時間09:00）に切り替わります。上限時も手入力とCalendar同期は基本の時間割で続けられます。[利用上限・CSP・追加監査](engineering/security-hardening-2026-11-07.md)を参照してください。

対象プロジェクトは `devfest-2026-513816`（番号 `1032251723022`）です。課金アカウントと予算はユーザー設定済みのものを使います。

1. Firestore NativeデータベースとFirebaseのWebアプリを作成し、AuthenticationのGoogleプロバイダーを有効にします。Cloud Runのドメインを認証の許可ドメインへ追加します。Firestoreのクライアント用ルールは `deploy/firestore.rules` のとおり拒否し、APIのサービスアカウントだけで読み書きします。
2. Calendar API、Secret Manager API、Vertex AI API、Cloud Run / Cloud Build / Artifact Registry APIを有効にします。
3. Web用OAuthクライアントを作成し、リダイレクトURIに `https://<管理画面のオリジン>/api/calendar/callback` を設定します。スコープは `openid`、`email`、`calendar.app.created`。テスト公開中は利用者をテストユーザーへ追加します。
4. `deploy/cloudrun.env.example.yaml` の公開設定を埋め、秘密情報を含まないローカル設定ファイルへ保存します。OAuth client secretとScheduler secretはSecret Managerへ登録します。`.env`、トークン、鍵はコミットしません。
5. 実行サービスアカウントにFirestore、Vertex AI、カレンダー用Secretの必要な権限を付与します。カレンダー接続時にユーザー別Secretを作るため、`secretmanager.secrets.create`、`secretmanager.versions.add`、`secretmanager.versions.access` を含む権限も必要です。公開設定・権限・イメージを確認してからCloud Runへ配信します。

Cloud Run向けのコンテナーは次で作成できます。

```bash
docker build -t dopagaki .
```

Cloud Runは `min-instances=0`、`max-instances=2`、1 CPU / 512MiB、リクエストタイムアウト60秒を初期値とします。`APP_MODE=live` が標準で、認証なしで個人データへアクセスできません。環境変数は `.env.example`、公開値の例は `deploy/cloudrun.env.example.yaml` を参照してください。`GOOGLE_OAUTH_CLIENT_SECRET` と `SCHEDULER_SECRET` はSecret Managerから環境変数へ渡します。

Calendarの変更通知チャンネルは6日で期限が切れます。Cloud Schedulerから1日1回 `POST /api/calendar/renew` を呼び、`Authorization: Bearer <SCHEDULER_SECRET>` を設定してください。この更新ジョブを設定しないと継続して同期できません。

管理画面のAI相談は既存のFirebase認証・Vertex AI・Firestoreを使います。新しいGoogle OAuthスコープやChatアプリの配布は不要です。会話履歴は `assistantConversations/{uid}/turns` に保存し、拡張向けの状態APIには含めません。カードの登録は既存の課題登録、変更は予定確認画面、記憶は本人の作業傾向へ接続します。[設計と確認範囲](engineering/assistant.md)を参照してください。

公開先が決まったら、そのオリジンに固定して拡張を再ビルドします。

```bash
APP_URL=https://<管理画面のオリジン> npm run build:extension
```

## 実サービスでの確認

2026年11月6日、架空の確認用課題を使って以下を確認しました。課題・報酬の確認履歴はテスト用です。

| 対象 | 確認した動作 |
|---|---|
| Cloud Run | 東京リージョンで公開。`/api/health` は200、未ログインの `/api/state` は401 |
| Firebase / Firestore | Googleログイン、初回60分、再読み込み後の予定・残高・一時停止タイマーの復元 |
| Vertex AI | Cloud Runの実行アカウントから課題抽出、具体的な手順作成、指示による時間割調整 |
| Google Calendar | 専用カレンダーの作成、Secret Managerへのトークン保存、詳細欄への手順反映、変更通知からの自動取り込みと登録報酬。既存予定の更新では報酬が増えないことも確認 |
| Geminiアプリ | 専用DOPAGAKIカレンダーへの予定作成。その後の自動取り込みと詳細欄への反映も確認 |
| 旧Google Chat | 当時の個人用アプリの検証記録。現在はアプリを無効化し、受付APIを削除 |
| Chrome拡張 | 実際のYouTube Shortsの前面再生で50秒消費。一時停止中は残高が変わらないことを確認 |
| 通知・作業 | 実時刻の全画面通知、事前候補からのスヌーズ確認、手動調整、OK後の保存、上部の作業バー、完了時20分の付与 |
| Cloud Scheduler | 毎日09:00（日本時間）のCalendar監視更新を設定。手動実行も成功 |

カレンダー側の新規予定は、専用Calendar APIからの投入に加え、一般画面での手入力・保存からの自動取り込みも確認しました。[実画面と検証条件](engineering/intake-flow.md)を保存しています。ローカルの拡張E2Eは合成Shorts DOMを使用し、上表の実YouTubeでの確認とは区別しています。

初期確認時のコードは `a25837c`、リビジョンは `dopagaki-00006-btr` です。最新の配信は[運用記録](engineering/operations.md)を参照してください。1 CPU / 512MiB、最小0・最大2インスタンス。Firestoreの直接クライアントアクセスは拒否し、API実行アカウントで読み書きします。課金・予算設定は変更していません。

## 検証

```bash
npm run build
npm test
(cd api && go vet ./...)
npx playwright install chromium
npm run test:e2e
```

Goのテストでは、並行再送時の報酬、スヌーズ候補、競合の拒否、時間の重複、他ユーザーとの分離を確認します。Gemini SDKはローカルHTTP応答を使い、本人情報が渡ることと不正な時間割を拒否することを確認します。Playwrightは画面操作と、パッケージ化していない拡張の接続・計測・通知・タイマーを検証します。有料の外部APIは呼びません。

Vertex AIの実接続テストは明示した場合だけ実行します。`gcloud` のログイン済みアカウントで、指定プロジェクトのGeminiへ架空の課題を送信します。このテストはAPI利用料金が発生し、通常のテストやCIではスキップされます。

```bash
cd api
DOPAGAKI_LIVE_GCP=1 go test ./internal/app -run TestLiveGeminiPlanning -v -count=1
```

## 提出に向けて

開催条件は添付キックオフPDFの「実行基盤の必須条件」「AI・他社ツールの扱い」「その他のルール」「提出ルール」を参照しています。Cloud Runでの稼働確認、GitHubコード、説明ドキュメント、デモ動画、著作物利用シートを揃える必要があります。11月10日17:00のコードフリーズ後は提出リポジトリを変更せず、デプロイURLは11月14日まで維持します。素材・ライブラリの出典は [ASSETS.md](ASSETS.md) に記録しています。チームの提出操作や共有設定はまだ行っていません。
