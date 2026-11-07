# 開発の進め方

DOPAGAKIの開発に参加するときの手順と決まりごとです。アプリの概要と動作は [README](README.md)、設計判断や検証の記録は [engineering/](engineering/README.md) を参照してください。`docs/` は別案の資料で、このアプリの仕様ではありません。

## 準備

Node.js 24.15以上とGo 1.26以上が必要です。

```bash
npm ci
```

## 起動

| 対象 | コマンド | 補足 |
|---|---|---|
| API と管理画面をまとめて | `./dev.sh` | ブラウザーも開く。Goが無ければ `.local/go` に取得する |
| API だけ（:8080） | `npm run dev:api` | デモモード。Google認証や有料APIを使わない |
| 管理画面だけ（:4200） | `npm run dev:web` | `/api` は :8080 へ中継するので、先にAPIを起動する |
| Chrome拡張 | `npm run build:extension` | `chrome://extensions` で `dist/extension` を読み込む。変更後は再ビルドして再読み込み |

デモのデータはAPIのメモリーにあり、APIを止めると消えます。

## テスト

```bash
npm test            # 型チェック・単体テスト・Goのテスト
npm run test:e2e    # Playwright（初回は npx playwright install chromium）
(cd api && go vet ./...)
npm run format      # Prettier
```

Vertex AIへの実接続テストは料金がかかるため、通常は実行しません。必要なときだけ `DOPAGAKI_LIVE_GCP=1` を付けて実行します（[README](README.md#検証)）。

## 作業の流れ

1. 必要ならIssueを作ります（不具合・提案のテンプレートがあります）。
2. 最新のmainからブランチを切ります。
3. 変更し、テストを通します。
4. PRを作ります。テンプレートに沿って、背景・変更・確認したことを書きます。
5. CIが通り、レビューを受けたらmainへマージします。

mainへ直接pushしないでください。

### ブランチ名

`種類/内容` の形にします。種類はコミットと同じです。

```text
feat/onboarding-tutorial
fix/sleep-display
docs/contributing
```

### コミットメッセージ

`種類: 日本語の要約` の形にします。要約は「何をしたか」がわかる一文にします。

| 種類 | 使う場面 |
|---|---|
| `feat` | 機能の追加・変更 |
| `fix` | 不具合の修正 |
| `perf` | 性能・費用の改善 |
| `test` | テストだけの追加・修正 |
| `docs` | 文書だけの変更 |
| `chore` | 依存関係・設定・mainの取り込みなど |

```text
fix: カレンダーで消された就寝予定を再保存で戻す
feat: スヌーズを確定したとき相棒の一言を出す
```

## mainへのマージは本番への配信になる

`api/` `web/` `shared/` `Dockerfile` などを変えたPRをmainへマージすると、CIの検証が通ったあと **Cloud Runへ自動で配信** されます（[継続的デプロイ](engineering/continuous-deployment.md)）。文書だけ・拡張だけの変更では配信しません。

- Firestoreの保存形式を変える場合は、既存ユーザーのデータを読めることを確かめてからマージします。
- 配信直後の確認に失敗した場合は、自動で前のリビジョンへ戻ります。それ以外で公開環境に問題が出たら、すぐにチームへ知らせてください（[運用記録](engineering/operations.md)）。

Chrome拡張は自動では配布されません。`vX.Y.Z` のタグをpushすると、GitHub Releasesに公開されます（[README](README.md#chrome拡張のリリース)）。

## 記録を残す

設計判断・不具合の原因・実サービスでの確認を行ったら、同じPRで `engineering/` の関連する文書を更新します。書き方は [engineering/README.md](engineering/README.md#今後の記録方法) にあります。発表と提出の説明は、この記録を根拠にします。

## 秘密情報と個人情報

- `.env`、鍵、トークン、OAuthのcodeはコミットしません。秘密の値はSecret Managerに置きます。
- Issue・PR・スクリーンショット・記録に、個人の予定、カレンダーID、メールアドレス、閲覧履歴を載せません。確認には架空の課題を使います。
- 素材やライブラリを追加したら、出典と利用条件を [ASSETS.md](ASSETS.md) に記録します。

## 提出までの予定

- 11/10 17:00：コードフリーズ。以降、提出リポジトリは変更しません。
- 11/14：Regional Day。それまで公開環境を動かし続けます。

締切が近い時期は、大きな作り直しより、小さく検証できる変更を優先します。
