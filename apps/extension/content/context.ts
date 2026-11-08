// content script の中で共有する状態と、background とのやりとり。
// 状態は background から受け取ったものを表示に使うだけで、ここでは書き換えない。
import type { CompanionMoment } from '@dopagaki/contract';
import { currentLocale, currentMessages, followLocale } from '../lib/locale';
import type { OkReply, PortPush } from '../lib/protocol';
import type { ExtensionState } from '../lib/schemas';
import { showToast } from '../ui/toast';
import { keepPort, send } from './messaging';

export class ContentContext {
  #state: ExtensionState | null = null;
  #ownsViewing = false;
  readonly #listeners = new Set<() => void>();

  constructor(readonly root: HTMLElement) {}

  get state(): ExtensionState | null {
    return this.#state;
  }

  get ownsViewing(): boolean {
    return this.#ownsViewing;
  }

  onChange(listener: () => void): void {
    this.#listeners.add(listener);
  }

  #set(state: ExtensionState | null, ownsViewing: boolean): void {
    this.#state = state;
    this.#ownsViewing = ownsViewing;
    // 「動きを減らす」設定は、ダイアログだけでなくタイマーや読み込み中の表示にも効かせる。
    this.root.classList.toggle('reduced', state?.settings?.reducedMotion === true);
    // 文言はプロフィールの言語で出す。ページ（YouTube など）の言語と違うことがあるので、読み上げ用に lang も付ける。
    followLocale(state);
    this.root.lang = currentLocale();
    for (const listener of this.#listeners) listener();
  }

  /** 操作の結果に含まれる最新の状態を反映する。 */
  apply(reply: OkReply): void {
    if (!reply.connected) {
      this.#set(null, false);
      return;
    }
    if (reply.state) this.#set(reply.state, reply.ownsViewing ?? this.#ownsViewing);
  }

  /** background から届いた状態。 */
  push(push: PortPush): void {
    this.#set(push.state, push.ownsViewing);
  }

  /** 状態を取り直す。`refresh` なら API から取り直す。成功したら true。 */
  async refresh(refresh: boolean): Promise<boolean> {
    const reply = await send({ type: 'GET_STATE', refresh });
    if (reply.ok) this.apply(reply);
    return reply.ok;
  }

  /** background とのポートを開き、状態の変化を受け取る。 */
  connect(): () => void {
    return keepPort((push) => {
      this.push(push);
    });
  }

  toast(text: string): void {
    showToast(this.root, text);
  }

  /** 相棒の一言。届かなければ決まった文。 */
  async line(moment: CompanionMoment, taskId?: string): Promise<string> {
    const reply = await send(
      taskId ? { type: 'COMPANION_LINE', moment, taskId } : { type: 'COMPANION_LINE', moment },
    );
    return reply.ok && reply.line
      ? reply.line
      : currentMessages().shared.companionFallbacks[moment];
  }
}
