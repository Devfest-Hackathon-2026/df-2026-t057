import { TestBed } from '@angular/core/testing';
import { ApiError, NetworkError } from '@dopagaki/contract';
import { messages } from '@dopagaki/messages';
import { flush } from '../../../testing/fake-backend';
import { AccountScope } from '../auth/account-scope';
import { Operation } from './operation';

describe('Operation', () => {
  // Operation は LocaleStore を注入するので、注入できる場所で作る。
  function createOperation(scope?: AccountScope): Operation {
    return TestBed.runInInjectionContext(() => new Operation(scope));
  }

  it('成功したら結果を反映し、エラーを消す', async () => {
    const operation = createOperation();
    operation.error.set({
      message: '前の失敗',
      code: undefined,
      status: undefined,
      cause: undefined,
    });
    const applied: number[] = [];
    const outcome = await operation.run(
      () => Promise.resolve(1),
      (value) => applied.push(value),
    );
    expect(outcome).toEqual({ ok: true, value: 1 });
    expect(applied).toEqual([1]);
    expect(operation.error()).toBeUndefined();
  });

  it('失敗を code から文言にして持つ', async () => {
    const operation = createOperation();
    const outcome = await operation.run(() =>
      Promise.reject(
        new ApiError({ type: 'about:blank', status: 412, code: 'precondition.failed' }),
      ),
    );
    expect(outcome.ok).toBe(false);
    expect(operation.error()?.message).toBe(messages.ja.shared.problems['precondition.failed']);
    expect(operation.error()?.code).toBe('precondition.failed');
    expect(operation.error()?.status).toBe(412);
  });

  it('通信の失敗は通信の文言にする', async () => {
    const operation = createOperation();
    await operation.run(() => Promise.reject(new NetworkError(new TypeError('offline'))));
    expect(operation.error()?.message).toBe(messages.ja.shared.network);
  });

  it('実行中に呼ばれても捨てずに順に実行する', async () => {
    const operation = createOperation();
    const order: string[] = [];
    let releaseFirst: () => void = () => undefined;
    const first = operation.run(
      () =>
        new Promise<string>((resolve) => {
          releaseFirst = () => {
            resolve('first');
          };
        }),
      (value) => order.push(value),
    );
    const second = operation.run(
      () => Promise.resolve('second'),
      (value) => order.push(value),
    );
    expect(operation.pending()).toBe(true);
    await flush();
    releaseFirst();
    await Promise.all([first, second]);
    expect(order).toEqual(['first', 'second']);
    expect(operation.pending()).toBe(false);
  });

  it('通信中にアカウントが切り替わったら結果を反映しない', async () => {
    const scope = new AccountScope();
    const operation = createOperation(scope);
    const applied: string[] = [];
    let release: () => void = () => undefined;
    const pending = operation.run(
      () =>
        new Promise<string>((resolve) => {
          release = () => {
            resolve('old');
          };
        }),
      (value) => applied.push(value),
    );
    await flush();
    scope.reset();
    release();
    const outcome = await pending;
    expect(outcome.ok).toBe(false);
    expect(applied).toEqual([]);
    expect(operation.error()?.message).toBe(messages.ja.web.app.operation.accountSwitched);
  });

  it('アカウントが切り替わるとエラーを消す', () => {
    const scope = new AccountScope();
    const operation = createOperation(scope);
    operation.error.set({ message: 'x', code: undefined, status: undefined, cause: undefined });
    scope.reset();
    expect(operation.error()).toBeUndefined();
  });
});
