import { Injectable, computed, signal } from '@angular/core';
import { newIdempotencyKey, type AssistantTurn, type Resource } from '@dopagaki/contract';
import { problemMessages } from '@dopagaki/messages';
import { idempotent, requireData } from '../../core/http/api-client';
import { Operation, type Outcome } from '../../core/http/operation';
import { ResourceStore } from '../../core/store/resource-store';

/** 画面に出す相談。サーバーの相談に、送信中・送信に失敗した手元の相談を重ねる。 */
export interface DisplayTurn {
  readonly turn: AssistantTurn;
  /** 失敗したときに出す文。 */
  readonly failureMessage: string | undefined;
}

/** 相談の入力欄の下書きを分けて持つ場所。 */
export type ComposerKey = 'workspace' | 'dialog';

/**
 * AI 相談の会話。
 * - 送信する相談の ID を `Idempotency-Key` に使う。失敗した相談を同じ内容で送り直すときは同じ ID を使う。
 * - 入力欄の下書きは store に持ち、画面の切り替えや移動で消えないようにする。
 */
@Injectable({ providedIn: 'root' })
export class AssistantStore extends ResourceStore<readonly AssistantTurn[]> {
  protected readonly resources: readonly Resource[] = ['assistant'];
  readonly send = new Operation(this.scope);

  /** 手元にだけある相談（送信中、または通信に失敗したもの）。 */
  private readonly local = signal<readonly DisplayTurn[]>([]);
  private readonly drafts = signal<Partial<Record<ComposerKey, string>>>({});
  /** 「この内容を覚える」で保存したカードの ID。 */
  readonly rememberedCards = signal<ReadonlySet<string>>(new Set());

  /** サーバーが返した相談の件数（直近20件までを返す）。 */
  readonly serverCount = computed(() => this.value()?.length ?? 0);

  readonly turns = computed<readonly DisplayTurn[]>(() => {
    const server = this.value() ?? [];
    const ids = new Set(server.map((turn) => turn.id));
    return [
      ...server.map((turn) => ({
        turn,
        failureMessage: turn.failure ? problemMessages[turn.failure] : undefined,
      })),
      ...this.local().filter((item) => !ids.has(item.turn.id)),
    ];
  });

  protected async fetch(): Promise<readonly AssistantTurn[]> {
    const result = await requireData(this.api.GET('/assistant/turns'));
    return result.turns;
  }

  protected override onReset(): void {
    this.local.set([]);
    this.drafts.set({});
    this.rememberedCards.set(new Set());
  }

  draft(key: ComposerKey): string {
    return this.drafts()[key] ?? '';
  }

  setDraft(key: ComposerKey, text: string): void {
    this.drafts.update((drafts) => ({ ...drafts, [key]: text }));
  }

  markRemembered(cardId: string): void {
    this.rememberedCards.update((ids) => new Set([...ids, cardId]));
  }

  /**
   * 相談を送る。直前の相談が同じ内容で失敗していれば、同じ ID で送り直す。
   * 返答が ready になり、入力欄が送った内容のままなら、下書きを消す。
   */
  sendMessage(
    text: string,
    taskId: string | undefined,
    composer: ComposerKey,
  ): Promise<Outcome<AssistantTurn>> {
    const last = this.turns().at(-1)?.turn;
    const id =
      last?.status === 'error' && last.text === text && last.taskId === taskId
        ? last.id
        : newIdempotencyKey();
    return this.deliver(id, text, taskId, composer);
  }

  /** 失敗した相談を、同じ ID で送り直す。 */
  retry(turn: AssistantTurn, composer: ComposerKey): Promise<Outcome<AssistantTurn>> {
    return this.deliver(turn.id, turn.text, turn.taskId, composer);
  }

  private deliver(
    id: string,
    text: string,
    taskId: string | undefined,
    composer: ComposerKey,
  ): Promise<Outcome<AssistantTurn>> {
    const pending: AssistantTurn = {
      id,
      text,
      status: 'pending',
      reply: '',
      cards: [],
      createdAt: new Date().toISOString(),
      ...(taskId === undefined ? {} : { taskId }),
    };
    this.putLocal({ turn: pending, failureMessage: undefined });
    return this.send
      .run(
        () =>
          requireData(
            this.api.POST('/assistant/turns', {
              params: { header: idempotent(id) },
              body: taskId === undefined ? { text } : { text, taskId },
            }),
          ),
        (turn) => {
          this.putLocal({
            turn,
            failureMessage: turn.failure ? problemMessages[turn.failure] : undefined,
          });
          this.value.update((turns) =>
            turns ? [...turns.filter((item) => item.id !== turn.id), turn] : turns,
          );
          if (turn.status === 'ready' && this.draft(composer).trim() === text.trim()) {
            this.setDraft(composer, '');
          }
        },
      )
      .then((outcome) => {
        // 通信に失敗した相談は、手元に「失敗」として残して送り直せるようにする。
        if (!outcome.ok) {
          this.putLocal({
            turn: { ...pending, status: 'error' },
            failureMessage: outcome.error.message,
          });
        }
        return outcome;
      });
  }

  private putLocal(item: DisplayTurn): void {
    this.local.update((items) => [
      ...items.filter((other) => other.turn.id !== item.turn.id),
      item,
    ]);
  }
}
