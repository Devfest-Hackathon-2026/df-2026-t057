# 残課題への対応と本番設定の読み取り確認

追記：ユーザー承認後のIAM適用・修正版配信・AI疎通1回は[本番反映の記録](security-rollout-2026-11-07.md)を参照。以下は追加実装・適用前確認時点の記録を保持している。

2026-11-07、日本時間。[初回確認](security-review-2026-11-07.md)への追加対応。`codex/security-refactor` の `74f986f` からの追加変更を専用worktreeで実装・検証した。本番のIAM・配信・課金設定は変更していない。

## AIの利用上限

ユーザー指定の「少なめ」を初期値にした。

| 単位 | 分あたり | 日あたり |
|---|---:|---:|
| 利用者1人 | 5回 | 30回 |
| サービス全体 | 30回 | 300回 |

Geminiの呼び出し直前に、Firestoreのトランザクションで本人と全体のカウンターを同時予約する。課題抽出、手順作成、予定調整、相談、Calendar同期を同じ生成関数へ接続した。本文のUIDは使わず、検証済みUIDまたは確認済みCalendar接続のUIDを明示して渡す。

予約1回につき生成リクエスト1回とし、SDKの自動再試行を無効にした。上流の失敗も数え、制限による拒否では回数を増やさない。保存失敗時は503で止め、Geminiを呼ばない。保存待ちは5秒まで。初回のエミュレーターテストでロック競合を観測したため、全クライアントが全体カウンターを先に読む順序へ統一した。

分次はUTCの各分、日次はUTC 00:00（日本時間09:00）に切り替わる固定窓。任意の連続60秒の上限や円単位の課金上限ではなく、FirestoreやHTTPリクエスト自体の費用も対象外。`AI_USER_PER_MINUTE`、`AI_USER_PER_DAY`、`AI_GLOBAL_PER_MINUTE`、`AI_GLOBAL_PER_DAY`で正の整数へ変更できる。0による無制限化は受け付けない。

課題抽出・予定調整・相談の上限時は429と`Retry-After`を返す。登録・編集・Calendar同期で手順作成の上限に達した場合は、Geminiを追加で呼ばず、締切と必要時間から基本の時間割を作る。その理由を`plan.reason`へ残す。UIでは429後も未送信文と既存予定を保持する。

実装：[ai_usage.go](../api/internal/app/ai_usage.go)、[planner.go](../api/internal/app/planner.go)、[assistant.go](../api/internal/app/assistant.go)。利用状況はクライアントから読めない`aiUsage`コレクションへ保存し、状態APIには含めない。

## CSPとログ

文書ごとに乱数nonceを発行し、Angularが生成したscript・styleとルート要素へ同じ値を渡す。文書は`no-store`とし、再読み込み時にnonceを変える。`script-src`はnonceと`strict-dynamic`を使い、`unsafe-inline`・`unsafe-eval`を許可しない。

style要素は同一オリジンまたはnonce付きに限定する。FullCalendar等が使うstyle属性だけは`style-src-attr 'unsafe-inline'`で許可する。Firebase認証の通信先・認証iframeは必要な宛先へ限定し、オブジェクト埋め込み・他ページからのフレーム化・カメラ等の権限を拒否する。PrivacyページのCSSも外部化した。

Angularがビルド時にscriptへnonceを付けるため、サーバーで属性を重ねて追加しない。`npm run dev:api`は`DEV_API_ONLY=true`のデモAPIとし、Webビルド前でも起動できる。本番モードではこのフラグを無視し、Web成果物とCSPを必須にする。

SDKエラーには応答本文・トークン・URL・利用者入力が含まれ得るため、サーバーログにも`err.Error()`を記録しない。操作名・エラー型・HTTPステータス・gRPCコードだけを残す。エラー本文を含むfixtureでログにも出ないことを確認した。

実装：[web.go](../api/internal/app/web.go)、[http.go](../api/internal/app/http.go)。方式の根拠：[AngularのCSP説明](https://angular.dev/best-practices/security#content-security-policy)。

## 本番設定の観測

| 対象 | 読み取りで確認した内容 |
|---|---|
| Cloud Run | `dopagaki-main-b6dffb4`へ100%配信。実行SAは`dopagaki-runtime`。最大2インスタンス。追加修正は未配信 |
| 公開範囲 | Invokerに`allUsers`。画面・Webhookの到達性とは別に個人データAPIでFirebase認証 |
| 未認証HTTP確認 | `/api/health`は200、`/api/state`は401。応答本文の本人情報は取得していない |
| Firestore | 適用済み`cloud.firestore`ルール本文が`deploy/firestore.rules`と一致し、クライアント読み書きを全面拒否 |
| Firebase許可ドメイン | Firebaseの`firebaseapp.com`・`web.app`、READMEの公開Cloud Runドメイン、`localhost` |
| Runtime IAM | Vertex AI User、Datastore User、Secret作成1権限、接頭辞条件付きSecret読取・バージョン追加。RuntimeへのOwner・Editorは観測していない |
| SA代理利用 | Runtime SA上に`dopagaki-deploy`のService Account User。組織等の継承を含む完全な権限解析ではない |
| Secret | 3件の一覧取得が完了。静的OAuth・Scheduler Secret自身のポリシーに追加バインディングはなく、プロジェクトの条件付き権限で扱う |
| ログ | `_Default`バケットは30日保存。sinkは`_Default`・`_Required`。過去ログは削除していない |

隔離したローカルChromiumと公開Firebase設定で、Googleスクリプト200、認証iframe200、`accounts.google.com`のログイン画面への到達を確認した。CSP違反イベントは0件。アカウント選択、ログイン、Calendar再同意、本人データの保存は行っていない。異なる実利用者2人の本番確認は主張しない。

## IAM縮小の準備

Vertex AI Userは確認時466権限、Datastore Userは23権限を含んでいた。[変更案と適用手順](../deploy/iam/README.md)では、SDKが使う推論1権限・Firestoreデータ操作6権限へ絞る。Secret追加先から静的OAuth・Schedulerを外し、読取先もCalendar用の名前空間とSchedulerへ絞る。Secretのバージョンを指す名前も条件に含める。

現在のポリシーから、他メンバー・バインディング・etagを保持した変更ファイルを作成した。対象バインディングだけを戻すファイル生成も確認した。条件式はGoogleのIAMリンターで検査し、指摘0件。**変更案の準備であり、本番適用・実権限での成功確認ではない。**

別のCompute標準SAにはプロジェクトEditorが残っていた。Compute APIは無効で、一覧確認したCloud Runは専用Runtime SAを使っていたが、これだけで他サービスからの利用がないと断定しない。利用元を確認せずこのSAの権限を削除しない。

## コンテナーと履歴の追加監査

配信中のdigest `sha256:7c3cc7f2948ffc08f5ee6ff6ebe918438c1e3f0334aab1f9890c250a772dfecc`をTrivy 0.76.2で読み取り検査した。

| 配信中イメージの検出 | 件数 | 追加対応 |
|---|---:|---|
| go-jose | High 1、Medium 2 | 同じ4.1系の公開最新版`v4.1.9`へ更新 |
| OpenPGPのモジュール報告 | Unknown 1 | 初回修正のx/crypto更新を保持 |
| ca-certificates・tzdataのDebian更新報告 | Unknown 4 | Debian 12の最新ベースにも残ったため、公式Distroless static-debian13 nonrootへ更新・digest固定 |

ソースの到達可能性を検査する`govulncheck`の0件と、コンテナー内モジュール・OSを検査するTrivyの結果は同じ範囲ではない。[go-jose公式報告](https://github.com/go-jose/go-jose/security/advisories)、[Distrolessの現行イメージ](https://github.com/GoogleContainerTools/distroless#what-images-are-available)を参照。

修正版のLinux/amd64静的バイナリ・Web成果物と固定した非rootベースを、ローカルのコンテナーtarへ組み立ててTrivyで検出0件を確認した。Dockerfile全段のDockerビルドや本番の起動試験の成功とは区別する。レジストリへのpushはしていない。

Gitleaks 8.30.1で、ローカル参照から到達できるGit履歴40コミット、約1.82 MBを`--redact=100`で検査し、検出0件。画像のOCRや検出規則にない形式まで保証するものではない。

## 検証結果

- `npm run build`、型確認、Node 4件、Go race、`go vet`が成功。
- Firestore公式エミュレーター1.22.0＋Goクライアント2つから12件の並行要求を実行。テスト用の利用者2回・全体3回の上限で予約成功は3件、全体・利用者カウンターが一致。新しいStoreインスタンスでも上限を保持。実Firestoreへの書き込みはしていない。
- Playwright 15件成功。通常画面・拡張操作、nonce更新、HTMLへ挿入したnonceなしscriptの拒否、429後の相談文・既存予定の保持を含む。
- `govulncheck ./...`と更新後ローカルイメージのTrivyは検出0件。

エミュレーター検証は、起動済みのループバックエミュレーターを指定した場合だけ実行する。

```bash
cd api
DOPAGAKI_EMULATOR_TEST=1 FIRESTORE_EMULATOR_HOST=127.0.0.1:8746 go test -race ./internal/app -run TestAIUsageAcrossFirestoreClients -count=1
```

## 適用前の境界

コードとローカル検証は完了。本番へ反映するには、修正版イメージの作成・アップロード・配信と、IAM変更案の適用を別途行う。稼働中の権限縮小と、有料AI・実OAuthを使う疎通は、具体的な適用内容を確認してから進める。

監査中、Firebase設定APIが返したパスワード用ハッシュ鍵を誤ってツール出力へ含めた。値はGitへ保存せず、監査ファイルからも除去し、以後の取得を`fields=authorizedDomains`へ限定した。パスワードハッシュや利用者一覧は取得していないが、会話のツール出力には残るため、履歴検査0件とは別の事象として記録する。認証設定や鍵の変更は行っていない。
