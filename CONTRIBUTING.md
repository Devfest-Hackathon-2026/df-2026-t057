# 開発の進め方

DOPAGAKIの開発に参加するときの手順と決まりごとです。アプリの概要と動作は [README](README.md)、設計判断や検証の記録は [engineering/](engineering/README.md) を参照してください。

## 準備

設計方針とコードの決まりごとは [AGENTS.md](AGENTS.md) にまとめています。変更の前に読んでください。

ツールのバージョンは [mise.toml](mise.toml) で固定しています（Node.js・pnpm・Go・golangci-lint）。[mise](https://mise.jdx.dev/) を入れてから、次を実行します。

```bash
mise install
pnpm install
```

## 起動

| 対象 | コマンド | 補足 |
|---|---|---|
| API と管理画面をまとめて | `pnpm dev` | デモモード。Google認証や有料APIを使わない |
| API だけ（:8080） | `pnpm --filter @dopagaki/api dev` | |
| 管理画面だけ（:4200） | `pnpm --filter @dopagaki/web dev` | `/api` は :8080 へ中継するので、先にAPIを起動する |
| Chrome拡張 | `pnpm --filter @dopagaki/extension build` | `chrome://extensions` で `apps/extension/.output/chrome-mv3` を読み込む。開発中は `pnpm --filter @dopagaki/extension dev` で自動で再読み込みされる |

デモのデータはAPIのメモリーにあり、APIを止めると消えます。

## 確認

```bash
pnpm generate     # API仕様（packages/contract/openapi.yaml）からGoとTSのコードを生成する
pnpm check        # フォーマット・Lint（Go / TS / CSS / API仕様 / コントラスト）・型チェック
pnpm test         # 単体テスト
pnpm test:e2e     # e2e（アクセシビリティの自動検査を含む。初回は pnpm --filter @dopagaki/e2e exec playwright install chromium）
pnpm format       # フォーマットを直す
```

CIでも同じものを実行します。生成物が古いまま、またはLint・フォーマットの違反があるPRはマージできません。

Vertex AIやGoogle Calendarへの実接続テストは料金がかかるため、通常は実行しません。必要なときだけ環境変数を付けて実行します。

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

`apps/api/` `apps/web/` `packages/` `Dockerfile` などを変えたPRをmainへマージすると、CIの検証が通ったあと **Cloud Runへ自動で配信** されます（[継続的デプロイ](engineering/continuous-deployment.md)）。文書だけ・拡張だけの変更では配信しません。

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
