// 色・文字サイズはトークンだけで指定する（AGENTS.md「デザインシステム」）。
/** @type {import('stylelint').Config} */
export default {
  extends: ['stylelint-config-standard-scss'],
  ignoreFiles: ['**/dist/**', 'packages/tokens/src/**'],
  rules: {
    'color-no-hex': true,
    'color-named': 'never',
    'function-disallowed-list': ['rgb', 'rgba', 'hsl', 'hsla', 'hwb', 'lab', 'lch', 'oklab', 'oklch'],
    'declaration-property-unit-allowed-list': {
      'font-size': ['rem', 'em', '%'],
      'line-height': ['rem', 'em', ''],
      'letter-spacing': ['em', 'rem'],
    },
    'declaration-property-value-disallowed-list': {
      outline: ['none', '0'],
      'letter-spacing': ['/^-/'],
    },
    'selector-disallowed-list': [['/\\.mat-mdc-/', '/\\.mdc-/'], { message: 'Materialの内部クラスを上書きしない。mat.<component>-overrides() を使う' }],
    'scss/at-extend-no-missing-placeholder': true,
    'selector-class-pattern': null,
    'custom-property-pattern': null,
    'no-descending-specificity': null,
  },
};
