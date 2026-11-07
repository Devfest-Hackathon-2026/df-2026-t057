# 実YouTube Shortsの持ち時間計測 — 2026-11-07

この文書は旧版`b6dffb4`の記録。同日後半に行ったmain `b3fc7b0`の視聴枠選択・途中終了の確認は[新しい視聴ルールの実YouTube確認](shorts-time-policy-verification-2026-11-07.md)を参照。

[PR #7](https://github.com/a-sumikawa/devfest-2026/pull/7)をユーザーの指示でマージし、Cloud Runへ配信した。その後、実際のChromeに導入済みの拡張と実YouTube Shortsを使い、持ち時間が減ることを確認した。今回、計測ロジックの修正は不要だった。

## 配信

| 対象 | 確認した値 |
|---|---|
| main | `b6dffb4a8f5ef4e89b8822ccbe739859b943e625` |
| mainのCI | [42326066750、成功](https://github.com/a-sumikawa/devfest-2026/actions/runs/42326066750) |
| Cloud Build | `3e45fd5f-1b91-4b70-8b20-37daf88de079`、成功 |
| Cloud Run | `dopagaki-main-b6dffb4`、100% |
| イメージdigest | `sha256:7c3cc7f2948ffc08f5ee6ff6ebe918438c1e3f0334aab1f9890c250a772dfecc` |
| 公開先 | https://dopagaki-1032251723022.asia-northeast1.run.app/ |

マージ済みコミットから必要なソースだけを`git archive`し、既存の`dopagaki-build`でDockerfileをビルドした。既存の実行SA・環境変数・Secret参照・リソース上限を維持し、最初は0%の候補リビジョンへ配信した。候補の`/api/health` 200、認証なし`/api/state` 401、`/calendar` 200を確認して100%へ切り替え、検証用タグを削除した。公開先でもhealth 200、認証なしstate 401を再確認した。

今回は手動配信。CD用[PR #6](https://github.com/a-sumikawa/devfest-2026/pull/6)は、このマージ許可の対象に含めず、未マージのまま保持した。課金・予算・IAM・OAuthスコープは変更していない。

## 実測

| 操作 | 結果 |
|---|---|
| 前面のShortsで再生を開始し、約30秒の待機後に一時停止 | 操作間の時間も含め、サーバーの持ち時間が38秒減少 |
| 同じShortsを一時停止したまま保持 | 残高取得の間隔約53秒で減少0秒。視聴受付数と最終視聴時刻も不変 |
| 管理画面を再読み込み | 減少後の残高を保持。画面の分表示は189から188へ変化 |
| 課題・報酬の比較 | 課題数と報酬台帳件数は不変 |

前面再生中の`POST /api/viewing`は、新しいリビジョンへ4回到達し、すべてHTTP 200だった。受付数も4件増えた。固定待機は30秒だが、再生ボタンから停止ボタンまでの実時間には操作・観測の時間も含むため、「30秒再生で38秒引かれた」と解釈しない。動画時刻を使った厳密な時間誤差の測定ではない。

準備中の再生で12秒、本計測で38秒、今回の確認では合計50秒を実残高から消費した。残高の直接編集や報酬での補填はしていない。終了時にShortsを閉じ、管理画面を前面に戻した。

実測を匿名化した記録は[evidence/shorts-2026-11-07.json](evidence/shorts-2026-11-07.json)。本人の残高の実値、メール、UID、動画URL・タイトル、認証情報、個人ブラウザーの画像はGitへ含めていない。

## どう確認したか

画面の分表示だけでは数秒の変化を捉えられないため、対象の確認用アカウントだけを[Identity Platformのaccounts.lookup](https://docs.cloud.google.com/identity-platform/docs/reference/rest/v1/projects.accounts/lookup)で特定し、そのユーザーのFirestore文書から残高・受付件数・最終視聴時刻・課題数・台帳件数を読み取って比較した。他ユーザーの一覧やデータは取得していない。読み取り用の管理権限を使い、ブラウザーから認証トークンを取り出したり、`/viewing`へ消費秒数を直接投入したりしていない。

再生・一時停止・再読み込みはChromeの実画面から操作した。今回の成功は、API単体の直接呼び出しや、ローカルE2Eの合成Shorts DOMによる成功とは別の実測である。

## 実装と注意点

- `extension/src/content.ts`は、ShortsのURL、可視状態、ページのフォーカス、再生中のvideo、読み込み状態、通知で覆われていないことを確認して消費時間を積算する。
- 通常は約10秒ごとに送り、停止・非表示になった場合は残った秒数も送る。画面の1秒ごとにFirestoreへ書く方式ではない。
- `extension/src/background.ts`は送信元タブがアクティブで、正規のYouTube Shortsであることを確認する。
- `api/internal/app/server.go`の`viewing`は、同じ受付IDの再送を重複計上せず、残高を超えて減算しない。
- 管理画面の分表示は丸め値、拡張ポップアップは切り捨て値。数十秒の確認では、分表示だけで消費量を判定しない。
- 開発用の`npm run build`は`dist/extension`も既定のlocalhost向けに作る。公開環境で使う前に、`APP_URL=https://dopagaki-1032251723022.asia-northeast1.run.app npm run build:extension`で作り直す必要がある。今回はこの公開設定で作り直して確認した。
- 初回はMacがロックされており、ユーザーに解除してもらってから実測した。ロック中の状態を前面視聴の根拠にはしていない。
- Chromeのタブ制御ではCDPのフォーカス設定がタイムアウトした。接続の切り分け後、同じChromeをmacOSのアクセシビリティから操作して確認した。別のブラウザーで拡張の成功を代用していない。

今回は通常動画、バックグラウンド再生、残高0での停止を再検証していない。また、新規のAI内容編集やGoogleカレンダーへの変更反映を本番で再確認した作業ではない。
