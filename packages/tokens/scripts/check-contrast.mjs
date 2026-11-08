#!/usr/bin/env node
// src/tokens.css のライト・ダーク両方の値について、WCAG 2.2 AA のコントラスト比を確かめる。
// あわせて、tokens.css の色が src/_theme.scss のパレット・拡張ロールと一致していることも確かめる。
// 依存なし。失敗があれば終了コード 1 で終わる。

import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const root = new URL('../', import.meta.url);
const css = readFileSync(fileURLToPath(new URL('src/tokens.css', root)), 'utf8');
const scss = readFileSync(fileURLToPath(new URL('src/_theme.scss', root)), 'utf8');

const HEX = '#[0-9a-fA-F]{6}';
const light = new Map();
const dark = new Map();
for (const m of css.matchAll(
  new RegExp(`(--[\\w-]+):\\s*(?:light-dark\\((${HEX}),\\s*(${HEX})\\)|(${HEX}))\\s*;`, 'g'),
)) {
  const [, name, l, d, both] = m;
  light.set(name, (l ?? both).toLowerCase());
  dark.set(name, (d ?? both).toLowerCase());
}

function luminance(hex) {
  const [r, g, b] = [1, 3, 5].map((i) => {
    const c = parseInt(hex.slice(i, i + 2), 16) / 255;
    return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

function ratio(a, b) {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

// [前景, 背景, 必要な比, 用途]
const pairs = [];
const names = [...light.keys()];
const has = (n) => light.has(n);

// on-X / X（文字 4.5:1）。on-X-fixed-variant は X-fixed の上に置く。
for (const name of names) {
  const m = name.match(/^(--[a-z]+-sys-)on-(.+)$/);
  if (!m) continue;
  const [, prefix, base] = m;
  const bg = base.endsWith('-fixed-variant')
    ? `${prefix}${base.replace(/-variant$/, '')}`
    : `${prefix}${base}`;
  if (has(bg)) pairs.push([name, bg, 4.5, 'text']);
}

const surfaces = [
  'surface',
  'surface-dim',
  'surface-bright',
  'surface-container-lowest',
  'surface-container-low',
  'surface-container',
  'surface-container-high',
  'surface-container-highest',
  'surface-variant',
].map((s) => `--mat-sys-${s}`);

for (const bg of surfaces) {
  // 本文・補足文字（on-surface / on-surface-variant）はどの面の上でも 4.5:1
  pairs.push(['--mat-sys-on-surface', bg, 4.5, 'text']);
  pairs.push(['--mat-sys-on-surface-variant', bg, 4.5, 'text']);
  // 入力欄の枠（outline）は 3:1（文字以外の要素）。surface-variant は枠を置く面ではないので除く
  if (bg !== '--mat-sys-surface-variant') pairs.push(['--mat-sys-outline', bg, 3, 'non-text']);
}
// 面の上に直接置くリンク・文字ボタン・強調色
for (const fg of ['primary', 'secondary', 'tertiary', 'error']) {
  pairs.push([`--mat-sys-${fg}`, '--mat-sys-surface', 4.5, 'text']);
  pairs.push([`--mat-sys-${fg}`, '--mat-sys-surface-container-highest', 4.5, 'text']);
}
pairs.push(['--mat-sys-inverse-on-surface', '--mat-sys-inverse-surface', 4.5, 'text']);
pairs.push(['--mat-sys-inverse-primary', '--mat-sys-inverse-surface', 4.5, 'text']);
for (const fg of ['notice', 'pachinko', 'mahjong']) {
  pairs.push([`--dpg-sys-${fg}`, '--mat-sys-surface', 3, 'non-text']);
}

const seen = new Set();
const uniquePairs = pairs.filter(([fg, bg]) => !seen.has(`${fg}|${bg}`) && seen.add(`${fg}|${bg}`));

let failures = 0;
const fmt = (n) => n.toFixed(2).padStart(5);
for (const [scheme, values] of [
  ['light', light],
  ['dark', dark],
]) {
  console.log(`\n# ${scheme}`);
  for (const [fg, bg, min, kind] of uniquePairs) {
    if (!values.has(fg) || !values.has(bg)) {
      console.log(`MISSING ${fg} on ${bg}`);
      failures++;
      continue;
    }
    const r = ratio(values.get(fg), values.get(bg));
    const ok = r >= min;
    if (!ok) failures++;
    console.log(`${ok ? 'ok  ' : 'FAIL'} ${fmt(r)}:1 (>= ${min}, ${kind})  ${fg} on ${bg}`);
  }
}

// --- tokens.css と _theme.scss の一致 ---
console.log('\n# sync with _theme.scss');
const paletteBlock = scss.slice(scss.indexOf('$_palettes:'), scss.indexOf('$_rest:'));
const paletteHexes = new Set(
  [...paletteBlock.matchAll(new RegExp(HEX, 'g'))].map((m) => m[0].toLowerCase()),
);
for (const name of names.filter((n) => n.startsWith('--mat-sys-'))) {
  for (const [scheme, values] of [
    ['light', light],
    ['dark', dark],
  ]) {
    if (!paletteHexes.has(values.get(name))) {
      console.log(
        `FAIL ${name} (${scheme}) ${values.get(name)} is not a tone of the palettes in _theme.scss`,
      );
      failures++;
    }
  }
}
const scssRoles = new Map(
  [...scss.matchAll(new RegExp(`^\\s+([a-z-]+): light-dark\\((${HEX}), (${HEX})\\),$`, 'gm'))].map(
    (m) => [`--dpg-sys-${m[1]}`, [m[2].toLowerCase(), m[3].toLowerCase()]],
  ),
);
const cssRoles = names.filter((n) => n.startsWith('--dpg-sys-'));
for (const name of new Set([...scssRoles.keys(), ...cssRoles])) {
  const s = scssRoles.get(name);
  const c = has(name) ? [light.get(name), dark.get(name)] : undefined;
  if (!s || !c || s[0] !== c[0] || s[1] !== c[1]) {
    console.log(`FAIL ${name} differs: _theme.scss=${s ?? '-'} tokens.css=${c ?? '-'}`);
    failures++;
  }
}
console.log(`${scssRoles.size} extension roles, ${names.length} color tokens checked`);

console.log(failures ? `\n${failures} failure(s)` : '\nAll checks passed.');
process.exit(failures ? 1 : 0);
