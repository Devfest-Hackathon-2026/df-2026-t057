// 視聴中の区切りの残り時間と「ここで終える」。区切りの持ち主のタブで、Shorts を開いているときだけ出す。
import { clockLabel, extensionCopy } from '@dopagaki/messages';
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

  constructor(
    private readonly root: HTMLElement,
    private readonly onStop: (container: HTMLElement) => void,
  ) {}

  update(model: ViewingBarModel): void {
    if (!model.visible) {
      this.#section?.remove();
      this.#section = undefined;
      return;
    }
    if (!this.#section) {
      const section = el('section', {
        className: 'viewing-bar',
        attrs: { 'aria-label': extensionCopy.viewingBarLabel },
      });
      this.#remaining = el('span', { className: 'remaining' });
      this.#button = button(extensionCopy.endHere, () => {
        this.onStop(section);
      });
      section.append(this.#remaining, this.#button);
      this.root.append(section);
      this.#section = section;
    }
    if (this.#remaining)
      this.#remaining.textContent = extensionCopy.viewingLeft(clockLabel(model.remainingSeconds));
    if (this.#button && !this.#section.hasAttribute('aria-busy'))
      this.#button.disabled = model.disabled;
  }
}
