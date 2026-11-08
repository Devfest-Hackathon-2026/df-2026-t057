// 視聴中の区切りの残り時間と「ここで終える」。区切りの持ち主のタブで、Shorts を開いているときだけ出す。
import { clockLabel, type Locale } from '@dopagaki/messages';
import { currentLocale, currentMessages } from '../lib/locale';
import { button, el } from './dom';

export interface ViewingBarModel {
  readonly visible: boolean;
  readonly remainingSeconds: number;
  readonly disabled: boolean;
}

export class ViewingBar {
  #section: HTMLElement | undefined;
  #remaining: HTMLElement | undefined;
  #button: HTMLButtonElement | undefined;
  #locale: Locale | undefined;

  constructor(
    private readonly root: HTMLElement,
    private readonly onStop: (container: HTMLElement) => void,
  ) {}

  update(model: ViewingBarModel): void {
    // 言語が変わったときも、ラベルとボタンを作り直す。
    if (this.#section && (!model.visible || this.#locale !== currentLocale())) {
      this.#section.remove();
      this.#section = undefined;
    }
    if (!model.visible) return;
    const copy = currentMessages().extension;
    if (!this.#section) {
      this.#locale = currentLocale();
      const section = el('section', {
        className: 'viewing-bar',
        attrs: { 'aria-label': copy.viewingBarLabel },
      });
      this.#remaining = el('span', { className: 'remaining' });
      this.#button = button(copy.endHere, () => {
        this.onStop(section);
      });
      section.append(this.#remaining, this.#button);
      this.root.append(section);
      this.#section = section;
    }
    if (this.#remaining)
      this.#remaining.textContent = copy.viewingLeft(clockLabel(model.remainingSeconds));
    if (this.#button && !this.#section.hasAttribute('aria-busy'))
      this.#button.disabled = model.disabled;
  }
}
