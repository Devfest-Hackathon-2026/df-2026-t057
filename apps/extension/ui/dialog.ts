// モーダルのダイアログ。<dialog> の showModal() でページの残りを操作できなくし、
// Tab キーでのフォーカスをダイアログの中で循環させ、Escape で閉じ、閉じたら元の場所へフォーカスを戻す。
import { el, focusable } from './dom';

export interface Dialog {
  readonly element: HTMLDialogElement;
  readonly panel: HTMLElement;
  /** 中身を入れたあとに呼ぶ。最初のボタン（または指定した要素）へフォーカスを移す。 */
  focusFirst(target?: HTMLElement): void;
  close(): void;
}

export interface DialogOptions {
  readonly label: string;
  readonly className: string;
  readonly panelClassName?: string;
  /** Escape や閉じるボタンで閉じたとき。 */
  readonly onClose?: () => void;
}

function deepActiveElement(): Element | null {
  let active = document.activeElement;
  while (active?.shadowRoot?.activeElement) active = active.shadowRoot.activeElement;
  return active;
}

export function openDialog(root: HTMLElement, options: DialogOptions): Dialog {
  const previous = deepActiveElement();
  const element = el('dialog', {
    className: options.className,
    attrs: { 'aria-label': options.label, 'aria-modal': 'true' },
  });
  const panel = el('div', { className: options.panelClassName ?? 'panel' });
  element.append(panel);
  let open = true;

  const close = () => {
    if (!open) return;
    open = false;
    element.close();
    element.remove();
    if (previous instanceof HTMLElement && previous.isConnected)
      previous.focus({ preventScroll: true });
  };

  element.addEventListener('cancel', (event) => {
    event.preventDefault();
    close();
    options.onClose?.();
  });
  element.addEventListener('keydown', (event) => {
    if (event.key !== 'Tab') return;
    const items = focusable(element);
    const first = items[0];
    const last = items[items.length - 1];
    if (!first || !last) return;
    const active = (element.getRootNode() as ShadowRoot | Document).activeElement;
    if (event.shiftKey && (active === first || !element.contains(active))) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && (active === last || !element.contains(active))) {
      event.preventDefault();
      first.focus();
    }
  });

  root.append(element);
  element.showModal();
  return {
    element,
    panel,
    focusFirst(target) {
      (target ?? focusable(panel)[0])?.focus();
    },
    close,
  };
}
