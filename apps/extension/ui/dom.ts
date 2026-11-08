// DOM を組み立てる小さな道具。文字は必ず textContent で入れる（innerHTML を使わない）。

export function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  options: { className?: string; text?: string; attrs?: Readonly<Record<string, string>> } = {},
  ...children: (Node | string)[]
): HTMLElementTagNameMap[K] {
  const element = document.createElement(tag);
  if (options.className) element.className = options.className;
  if (options.text !== undefined) element.textContent = options.text;
  for (const [name, value] of Object.entries(options.attrs ?? {}))
    element.setAttribute(name, value);
  element.append(...children);
  return element;
}

export type ButtonVariant = 'filled' | 'tonal' | 'outlined' | 'text';

export function button(
  text: string,
  onClick: () => void,
  variant: ButtonVariant = 'outlined',
  label?: string,
): HTMLButtonElement {
  const element = el('button', { className: `button ${variant}`, text });
  element.type = 'button';
  if (label) element.setAttribute('aria-label', label);
  element.addEventListener('click', onClick);
  return element;
}

/** フォーカスを移せる要素（ダイアログの中でフォーカスを循環させるのに使う）。 */
export function focusable(container: ParentNode): HTMLElement[] {
  return [
    ...container.querySelectorAll<HTMLElement>(
      'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])',
    ),
  ].filter((element) => !element.hasAttribute('disabled') && !element.closest('[hidden]'));
}
