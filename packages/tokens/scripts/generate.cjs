// トークンを生成し直す: pnpm --filter @dopagaki/tokens generate
// 種の色を変えるときは SEEDS / EXT を編集する。
const fs = require('fs');
const os = require('os');
const path = require('path');
const { execFileSync } = require('child_process');
const s = require(
  path.join(
    path.dirname(require.resolve('@angular/material/package.json')),
    'schematics/ng-generate/theme-color/index_bundled.js',
  ),
);
const PKG = path.resolve(__dirname, '..');
const WORK = fs.mkdtempSync(path.join(os.tmpdir(), 'dpg-tokens-'));
const hex = (a) => '#' + (a & 0xffffff).toString(16).padStart(6, '0');

const SEEDS = {
  primary: '#2858eb',
  tertiary: '#d4f777',
  neutral: '#5e615d',
  neutralVariant: '#797e76',
};
const EXT = { notice: '#f9d46b', pachinko: '#6b3908', mahjong: '#187855' };

const pal = s.getColorPalettes(
  SEEDS.primary,
  undefined,
  SEEDS.tertiary,
  SEEDS.neutral,
  SEEDS.neutralVariant,
  undefined,
);
const comment = `Color palettes are generated from primary: ${SEEDS.primary}, tertiary: ${SEEDS.tertiary}, neutral: ${SEEDS.neutral}, neutral variant: ${SEEDS.neutralVariant}`;
let palettesScss = s
  .generateSCSSTheme(pal, comment)
  .split('\n')
  .filter(
    (l) => !l.startsWith('@use') && !l.startsWith('// This file') && !l.startsWith('// Proceed'),
  )
  .join('\n')
  .trim();

const ext = {};
for (const [k, v] of Object.entries(EXT)) ext[k] = s.getColorPalettes(v).primary; // TonalPalette.fromHct(seed)
const ld = (l, d) => `light-dark(${hex(l)}, ${hex(d)})`;
const roles = [];
const add = (name, l, d, note) => roles.push({ name, value: ld(l, d), note });
// M3 カスタムカラーと同じトーン割り当て（mat.theme の primary 系と同じ）
for (const k of Object.keys(EXT)) {
  const p = ext[k];
  add(`${k}`, p.tone(40), p.tone(80));
  add(`on-${k}`, p.tone(100), p.tone(20));
  add(`${k}-container`, p.tone(90), p.tone(30));
  add(`on-${k}-container`, p.tone(30), p.tone(90));
}
// 没入型オーバーレイの背景（ライトでも暗いまま）と、その上の文字（金色の生成り）
for (const k of ['pachinko', 'mahjong']) {
  add(`${k}-scene`, ext[k].tone(20), ext[k].tone(10));
  add(`on-${k}-scene`, ext.notice.tone(95), ext.notice.tone(90));
}

const FONT = `('Noto Sans JP', 'Hiragino Sans', 'Hiragino Kaku Gothic ProN', 'Yu Gothic UI', Meiryo, system-ui, sans-serif)`;
const scss = `// DOPAGAKI の Angular Material（M3）テーマ。
// パレットは \`ng generate @angular/material:theme-color\`（@angular/material 22.2.6）と同じ手順で生成した。
// 色を変えるときはパレットを生成し直し、src/tokens.css も同じ値にそろえてから \`pnpm --filter @dopagaki/tokens check\` を実行する。

@use 'sass:map';
@use '@angular/material' as mat;

// Note: ${palettesScss.replace(/^\/\/ Note: x\n/, '')}

$font-family: ${FONT};

// M3 にない、アプリ固有の役割。パレットから M3 カスタムカラーと同じトーンで導いた。
// notice = 通知（金色）、pachinko / mahjong = モードごとの色。*-scene は没入型オーバーレイの背景。
$extension-roles: (
${roles.map((r) => `  ${r.name}: ${r.value},`).join('\n')}
);

@mixin dopagaki-theme() {
  color-scheme: light dark;

  @include mat.theme(
    (
      color: (
        theme-type: color-scheme,
        primary: $primary-palette,
        tertiary: $tertiary-palette,
      ),
      typography: (
        brand-family: $font-family,
        plain-family: $font-family,
        bold-weight: 700,
        medium-weight: 500,
        regular-weight: 400,
      ),
      density: 0,
    )
  );

  // 日本語に負の字間を付けない（M3 の display-large は -0.016rem）。
  @include mat.theme-overrides(
    (
      display-large-tracking: 0,
    )
  );

  @each $name, $value in $extension-roles {
    --dpg-sys-#{$name}: #{$value};
  }
}
`;
const final = scss.replace('// Note: // Note: ', '// Note: ');
fs.writeFileSync(`${PKG}/src/_theme.scss`, final);

// 実際にコンパイルして、出力された値から tokens.css を作る（値の一致を保証する）
fs.writeFileSync(
  path.join(WORK, 'compile.scss'),
  `@use 'theme' as dpg;\nhtml { @include dpg.dopagaki-theme(); }\n`,
);
const css = execFileSync(path.join(PKG, 'node_modules/.bin/sass'), [
  '--no-source-map',
  `--load-path=${path.join(PKG, 'node_modules')}`,
  `--load-path=${PKG}/src`,
  path.join(WORK, 'compile.scss'),
]).toString();
const decls = new Map();
for (const m of css.matchAll(/^\s*(--[\w-]+):\s*(.+);$/gm)) {
  decls.delete(m[1]);
  decls.set(m[1], m[2]);
}
const group = (pred) => [...decls].filter(([k]) => pred(k));
const isColor = (k, v) => v.startsWith('light-dark(') || /^#/.test(v);
const used = new Set();
const section = (title, entries) => {
  entries.forEach(([k]) => used.add(k));
  return `\n  /* ${title} */\n` + entries.map(([k, v]) => `  ${k}: ${v};`).join('\n') + '\n';
};
const colors = group((k) => k.startsWith('--mat-sys-') && isColor(k, decls.get(k)));
const typo = group((k) => /^--mat-sys-(display|headline|title|body|label)-/.test(k));
const elev = group((k) => /^--mat-sys-level\d$/.test(k));
const shape = group((k) => k.startsWith('--mat-sys-corner'));
const state = group((k) => /state-layer-opacity$/.test(k));
const dpg = group((k) => k.startsWith('--dpg-sys-'));
let out = `/*
 * DOPAGAKI のデザイントークン（フレームワークに依存しない CSS 変数）。
 * Chrome 拡張の Shadow DOM など、Angular Material を使えない場所で読み込む。
 * 値は src/_theme.scss（mat.theme の出力）と同じ。手で書き換えず、テーマと一緒に生成し直す。
 * ${comment}
 */
:root,
:host {
  color-scheme: light dark;
`;
out += section('Color', colors);
out += section('DOPAGAKI extension roles', dpg);
out += section('Typescale（日本語の本文は body-large）', typo);
out += section('Elevation', elev);
out += section('Shape', shape);
out += section('State', state);
const rest = [...decls].filter(([k]) => !used.has(k));
if (rest.length) out += section('Other', rest);
out += '}\n';
fs.writeFileSync(`${PKG}/src/tokens.css`, out);
console.log(
  'decls',
  decls.size,
  'rest',
  rest.map(([k]) => k),
);
