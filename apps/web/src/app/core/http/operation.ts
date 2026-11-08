import { computed, signal } from '@angular/core';
import { ApiError, type ProblemCode } from '@dopagaki/contract';
import { errorMessage } from '@dopagaki/messages';
import type { AccountScope } from '../auth/account-scope';

export const accountSwitchedMessage = 'アカウントが切り替わりました。操作をやり直してください。';

/** 操作に失敗したときに画面へ出す情報。 */
export interface OperationError {
  readonly message: string;
  readonly code: ProblemCode | undefined;
  readonly status: number | undefined;
  readonly cause: unknown;
}

export type Outcome<T> =
  { readonly ok: true; readonly value: T } | { readonly ok: false; readonly error: OperationError };

/** 通信中にアカウントが切り替わった。結果は捨てる。 */
export class AccountSwitchedError extends Error {
  constructor() {
    super('account switched');
    this.name = 'AccountSwitchedError';
  }
}

export function toOperationError(cause: unknown): OperationError {
  if (cause instanceof AccountSwitchedError) {
    return { message: accountSwitchedMessage, code: undefined, status: undefined, cause };
  }
  return {
    message: errorMessage(cause),
    code: cause instanceof ApiError ? cause.code : undefined,
    status: cause instanceof ApiError ? cause.status : undefined,
    cause,
  };
}

/**
 * 1種類の操作の通信状態とエラー（AGENTS.md「管理画面」：操作ごとに持つ）。
 * 実行中に呼ばれても捨てずに、前の実行が終わってから順に実行する。
 */
export class Operation {
  private readonly running = signal(0);
  private tail: Promise<unknown> = Promise.resolve();
  readonly pending = computed(() => this.running() > 0);
  readonly error = signal<OperationError | undefined>(undefined);

  constructor(private readonly scope?: AccountScope) {
    scope?.onReset(() => {
      this.error.set(undefined);
    });
  }

  /**
   * `request` を実行し、成功したら（アカウントが変わっていなければ）`apply` で結果を反映する。
   * 例外は投げず、結果を `Outcome` で返す。
   */
  async run<T>(request: () => Promise<T>, apply?: (value: T) => void): Promise<Outcome<T>> {
    this.running.update((count) => count + 1);
    const previous = this.tail;
    let release = (): void => undefined;
    this.tail = new Promise<void>((resolve) => {
      release = resolve;
    });
    try {
      await previous;
      this.error.set(undefined);
      const epoch = this.scope?.epoch();
      const value = await request();
      if (this.scope && this.scope.epoch() !== epoch) throw new AccountSwitchedError();
      apply?.(value);
      return { ok: true, value };
    } catch (cause: unknown) {
      const error = toOperationError(cause);
      this.error.set(error);
      return { ok: false, error };
    } finally {
      release();
      this.running.update((count) => count - 1);
    }
  }

  clearError(): void {
    this.error.set(undefined);
  }
}
