# @dopagaki/tokens

DOPAGAKIのMaterial Design 3トークン。webとChrome拡張で同じ値を使う。

| ファイル | 用途 |
|---|---|
| `src/_theme.scss`（`@dopagaki/tokens/theme`） | web（Angular Material）のテーマ |
| `src/tokens.css`（`@dopagaki/tokens/tokens.css`） | 拡張のShadow DOMなど、Angular Materialを使えない場所 |

どちらも同じ `--mat-sys-*`（M3の役割）と `--dpg-sys-*`（アプリ固有の役割）を定義する。ライト・ダークは `light-dark()` で切り替わる。

## 使い方

### web

```scss
@use '@dopagaki/tokens/theme' as dpg;

html {
  @include dpg.dopagaki-theme();
}

body {
  background: var(--mat-sys-surface);
  color: var(--mat-sys-on-surface);
  font: var(--mat-sys-body-large);
}
```

### 拡張（Shadow DOM）

```ts
import tokens from '@dopagaki/tokens/tokens.css?inline';

const sheet = new CSSStyleSheet();
sheet.replaceSync(tokens);
shadowRoot.adoptedStyleSheets = [sheet, ...shadowRoot.adoptedStyleSheets];
```

`:root` と `:host` の両方に定義しているので、ポップアップのページでもShadow DOMでも読み込むだけで使える。

## 役割の選び方

色は値ではなく役割で選ぶ。M3の役割どおりに組み合わせれば、コントラストはWCAG 2.2 AAを満たす。

| 用途 | トークン |
|---|---|
| 画面の背景 / 本文 | `surface` / `on-surface` |
| カード・一段下がった面 | `surface-container-low` 〜 `surface-container-highest` |
| 補足の文字・アイコン | `on-surface-variant` |
| 区切り線 | `outline-variant` |
| 入力欄の枠 | `outline` |
| 主なボタン | `primary` / `on-primary` |
| 選択中・強調の面 | `primary-container` / `on-primary-container` |
| ライム系の強調（達成・報酬） | `tertiary-container` / `on-tertiary-container` |
| エラー | `error` / `on-error`、`error-container` / `on-error-container` |
| スナックバー | `inverse-surface` / `inverse-on-surface`、操作は `inverse-primary` |
| 通知（金色） | `--dpg-sys-notice-container` / `--dpg-sys-on-notice-container` |
| パチンコ・麻雀モードの背景 | `--dpg-sys-{pachinko,mahjong}-scene` / `--dpg-sys-on-{pachinko,mahjong}-scene` |
| モードの強調色 | `--dpg-sys-{pachinko,mahjong}`、`-container` とその `on-` |

- 文字は `--mat-sys-body-large` などのタイプスケールで指定する。日本語の本文は `body-large`（1rem / 1.5rem）。`label-small` はバッジなど補助的な表示だけに使う。
- `display-large` の負の字間は0に上書きしている（日本語に負の字間を付けない）。
- 角丸は `--mat-sys-corner-*`、影は `--mat-sys-level0`〜`5` を使う。

## 色の元（seed）

`ng generate @angular/material:theme-color`（@angular/material 22.2.6）と同じ手順でパレットを生成した。

| パレット | seed | 元にした色 |
|---|---|---|
| primary | `#2858eb` | 青 |
| tertiary | `#d4f777` | ライム |
| neutral | `#5e615d` | 紙の背景 `#f7f8f5` に近い面になる低彩度のグレー |
| neutral variant | `#797e76` | 従来の補足文字の色 |
| secondary / error | 自動生成 | |
| notice（拡張） | `#f9d46b` | 通知の金色 |
| pachinko（拡張） | `#6b3908` | パチンコ演出の茶色 |
| mahjong（拡張） | `#187855` | 麻雀卓の緑 |

拡張の役割は、M3のカスタムカラーと同じトーンで割り当てる（ライト：色40・on 100・container 90・on-container 30／ダーク：80・20・30・90）。`*-scene` はライト20・ダーク10、その上の文字は金色パレットの95・90。

## 色を変えるとき

1. `scripts/generate.cjs` の `SEEDS`（M3の元の色）と `EXT`（拡張の役割の元の色）を編集する。
2. `pnpm --filter @dopagaki/tokens generate` を実行する。`src/_theme.scss` と、それをコンパイルした値から `src/tokens.css` を生成し直す。生成物は手で編集しない。
3. `pnpm --filter @dopagaki/tokens lint` を実行する。コントラスト（文字4.5:1、枠3:1）と、2つのファイルの値の一致を確かめる。CIでも実行する。
