# 素材・ライブラリ

提出用の著作物利用シートへ転記するための記録です。

| 対象 | 出典 | 利用方法・ライセンス |
|---|---|---|
| DOPAGAKIロゴ | ユーザー提供画像（2026-11-05の会話） | 原画像を切り抜き、配置・縮小。`web/public/logo.png`、拡張アイコン。権利の帰属・提出時の表記は提供者に確認する |
| 金色の光 | ユーザー提供参考画像（同会話） | 通知背景へ利用。`web/public/gold-light.png`。再配布可否は提供者に確認する |
| Material Symbols Rounded | Google / [Material Symbols](https://fonts.google.com/icons) | Apache-2.0。`material-symbols` パッケージから使用するアイコンを抽出し自己ホスト。ライセンスは `web/public/material-symbols-LICENSE.txt` |
| Google Gロゴ | Google / [ログインボタンの公式ガイド](https://developers.google.com/identity/branding-guidelines)、[公式PNG](https://developers.google.com/static/identity/images/g-logo.png) | `web/public/google-g.png`。Googleログインボタン内に使用。Googleのブランドガイドに従い、画像生成案のロゴは転用しない |
| Angular / Angular Material / CDK | [angular.dev](https://angular.dev/)、[material.angular.dev](https://material.angular.dev/) | MIT |
| Firebase JavaScript SDK / Firebase Admin Go SDK | [firebase.google.com](https://firebase.google.com/) | Apache-2.0 |
| Google Gen AI Go SDK、Google Cloud Go SDK | [Google Cloud](https://cloud.google.com/)、[googleapis/go-genai](https://github.com/googleapis/go-genai) | Apache-2.0 |
| Google API Go client、golang.org/x/oauth2 | Google提供GoパッケージのLICENSE | BSD-3-Clause |
| RxJS / tslib | npmパッケージのLICENSE | Apache-2.0 |
| Go標準ライブラリ | [go.dev](https://go.dev/) | BSD-3-Clause |

開発時のみ使うCLI、esbuild、TypeScript、Playwright、Prettier等の正確なバージョンは `package-lock.json`、Goの依存関係は `api/go.sum` で固定しています。製品のAI処理はVertex AIのGeminiを利用します。
