# セキュリティ修正の本番反映

2026-11-07、日本時間。ユーザーが「本番のIAM縮小・修正版配信・有料AI疎通1回」を承認した後に実施した。[追加実装の記録](security-hardening-2026-11-07.md)に対する本番での結果。

## 配信結果

| 項目 | 確認結果 |
|---|---|
| アプリのコード | `0b05f11`。`codex/security-refactor`で検証した追加修正 |
| 公開URL | https://dopagaki-1032251723022.asia-northeast1.run.app/ |
| 新リビジョン | `dopagaki-security-0b05f11`へ100% |
| 配信イメージ | `asia-northeast1-docker.pkg.dev/devfest-2026-513816/dopagaki/app@sha256:02c22e986a670a97e38e4c370575a5043aaa40f1751a668eccb3359cdcb3b2a0` |
| 戻し先 | `dopagaki-main-b6dffb4`。旧イメージ・リビジョンは保持 |
| Runtime SA | `dopagaki-runtime@devfest-2026-513816.iam.gserviceaccount.com`を維持 |
| CPU・メモリー・同時実行・タイムアウト | 1 CPU・512MiB・40・60秒を維持 |
| 既存設定 | 既存環境変数・Secret参照が一致。既存の最大インスタンス設定2も維持 |
| AI上限 | 1人5回/分・30回/日、全体30回/分・300回/日を環境変数へ明示 |

ローカルで監査したLinux/amd64のコンテナーtarを、既存レジストリへアップロードしてdigestを固定した。Cloud Build、GitHubへのpush・マージ、CDは使っていない。まず通常URLを旧版へ残したまま確認用タグで起動し、ヘルス・CSP・設定保持を確認した。権限とAI疎通の成功後に100%へ切り替え、最後に確認用タグを除去した。

## Runtime IAM

`roles/aiplatform.user`と`roles/datastore.user`を外し、次のカスタムロールへ置き換えた。他のメンバーのバインディングは変更前と一致することを確認した。

| 対象 | 適用後 |
|---|---|
| Gemini | `dopagakiGeminiInference`：`aiplatform.endpoints.predict`の1権限 |
| Firestore | `dopagakiDataAccess`：データ操作の6権限 |
| Secretの新規作成 | 既存の`dopagakiSecretCreator`を保持 |
| Secret読取 | Calendar用の名前空間とSchedulerに限定 |
| Secretバージョン追加 | Calendar用の名前空間に限定し、静的OAuth Secretを除外。Schedulerも対象外 |

適用直前に取得したetag付きポリシーから変更ファイルを作成して適用した。最終ポリシーでもRuntimeに旧2ロールがなく、カスタムロールの実際の権限数が1・6であることを再取得して確認した。別のCompute標準SAやOwner等の権限は変更していない。[定義・戻し方](../deploy/iam/README.md)を参照。

## 実行アカウントによる確認

Work設定でブラウザー操作が無効だった。また、操作アカウントにはRuntime SAの短期トークン発行権限がなかった。権限を追加せず、同じRuntime SAを指定した一度限りのCloud Run Jobで確認した。

- Job：`dopagaki-security-smoke-0b05f11`
- Execution：`dopagaki-security-smoke-0b05f11-qfcsd`
- 実行：タスク1件、並列1、自動再試行0。SDKの生成再試行も0
- 実行開始：2026-11-07 13:12:54日本時間。正常完了：13:13:10
- コマンド：[cmd/security-smoke](../api/cmd/security-smoke/main.go)。明示した有料確認フラグと確認用IDがなければ起動しない

| 確認 | 実測結果 |
|---|---|
| Vertex権限 | 権限確認APIで推論を許可、エンドポイント作成を不許可 |
| Firestore | 架空UIDの初期状態作成・読取・更新、相談の予約・照会・保存・読戻し・削除が成功 |
| 静的Secret | OAuth・Schedulerの使用中バージョンを読取成功。権限確認APIでバージョン追加・Secret削除は不許可 |
| ユーザー別Secretの名前空間 | ラベル付き確認用Secretの作成・バージョン追加・読戻しが成功。権限確認APIでSecret削除は不許可 |
| Gemini | 架空の確認文で1回応答。実利用者のプロフィール・予定・会話は送信していない |
| 利用カウンター | 確認前は全体の行なし。確認後は全体と確認用UIDの`DayCalls`がそれぞれ1 |
| 再送・自動実行 | `ai-start`ログは1件、成功タスク1件。有料確認の再実行はしていない |

操作カードは適用せず、実利用者の課題・Calendar予定を作成・変更していない。確認用の`users`・会話・会話メタデータの削除を確認した。Runtimeが削除できない確認用Secretは、操作アカウントで作成ラベルとIDを照合して削除した。Jobと専用イメージも削除した。AI利用カウンターは実際の利用記録として保持した。

この確認はRuntime SAのSDK操作・権限・生成・保存の確認であり、実利用者のブラウザー経由のログイン・Calendar再同意や、異なる実利用者2人の操作確認ではない。

## 通常URLでの最終確認

| 対象 | 結果 |
|---|---|
| `/api/health` | 200 |
| 未認証の`/api/state` | 401 |
| `/privacy.html`・`/privacy.css` | 200 |
| 公開設定 | live、AI有効、Calendar有効 |
| CSP | nonceが文書間で変わり、配信HTMLと一致。`strict-dynamic`あり、`unsafe-eval`なし |
| 文書キャッシュ・フレーム | `Cache-Control: no-store`、`X-Frame-Options: DENY` |
| トラフィック | 新リビジョン100%。確認用タグなし |

ローカル検証済みのCSP付き画面に対応するHTMLを本番で確認した。ブラウザー実画面操作はWork設定の制約で今回実施していない。開いている管理画面は再読み込みすると新しい文書とCSPが適用される。

## Chrome拡張

公開オリジンに固定した`dopagaki-extension-security-0b05f11.zip`をローカルへ作成した。資格情報や利用者データは含まない。ブラウザー操作が無効のため、既存Chromeプロフィールへの導入・更新は未実施。

ZIPを展開して既存の読み込み先を差し替えるか、展開先を「パッケージ化されていない拡張機能」として読み込む。その後、拡張と管理画面を読み込み直す。別の場所から新規導入する場合は旧拡張との二重動作を避ける。Cloud Runへの配信だけでは、導入済みの拡張のコードは更新されない。

## 証拠の扱い

IAM・配信前後の設定、実行結果、段階別ログ、カウンター、クリーンアップ結果をローカルの`.local/rollout-20261107/`へ保存した。共有用の結果は本文に集約し、秘密値・利用者情報は記録していない。レジストリ用の一時認証ファイルは消去済み。過去のハッシュ鍵のツール出力事象は[追加監査記録](security-hardening-2026-11-07.md#適用前の境界)のとおりで、この配信で認証鍵の変更は行っていない。
