import { InjectionToken, inject } from '@angular/core';
import {
  ApiError,
  createApiClient,
  etag,
  newIdempotencyKey,
  type ApiClient,
} from '@dopagaki/contract';
import { AuthService } from '../auth/auth.service';

/** アプリで1つだけ使う API クライアント。`Authorization` と `Idempotency-Key` を付ける。 */
export const API_CLIENT = new InjectionToken<ApiClient>('ApiClient', {
  providedIn: 'root',
  factory: () => {
    const auth = inject(AuthService);
    return createApiClient({
      origin: location.origin,
      authorization: () => auth.authorization(),
      // デモは Cookie で利用者を識別する（同じオリジン）。
      credentials: 'include',
    });
  },
});

interface FetchResult<T> {
  readonly data?: T;
  readonly response: Response;
}

/** 成功した応答の本文を返す。エラーはクライアントのミドルウェアが ApiError にして投げる。 */
export async function requireData<T>(request: Promise<FetchResult<T>>): Promise<T> {
  const { data, response } = await request;
  if (data === undefined) {
    throw new ApiError({ type: 'about:blank', status: response.status, code: 'internal' });
  }
  return data;
}

/** 本文のない応答（204）を待つ。 */
export async function requireOk(request: Promise<{ readonly response: Response }>): Promise<void> {
  await request;
}

/** 状態を変えるリクエストのヘッダー。再送するときは同じ `key` を渡す。 */
export function idempotent(key: string = newIdempotencyKey()): { 'Idempotency-Key': string } {
  return { 'Idempotency-Key': key };
}

/** 版を指定して更新するリクエストのヘッダー（`If-Match`）。 */
export function ifMatch(
  revision: number,
  key: string = newIdempotencyKey(),
): { 'Idempotency-Key': string; 'If-Match': string } {
  return { 'Idempotency-Key': key, 'If-Match': etag(revision) };
}
