// テスト用の偽の API。`createApiClient` の `fetch` に差し替えて使う。
import { signal, type Provider } from '@angular/core';
import { createApiClient } from '@dopagaki/contract';
import { AuthService } from '../app/core/auth/auth.service';
import { API_CLIENT } from '../app/core/http/api-client';
import { EVENT_SOURCE_FACTORY } from '../app/core/realtime/realtime.service';

export interface RecordedRequest {
  readonly method: string;
  /** `/api` を除いたパス（クエリを含む）。 */
  readonly path: string;
  readonly headers: Headers;
  readonly body: unknown;
}

export interface FakeResponse {
  readonly status?: number;
  readonly body?: unknown;
  readonly headers?: Record<string, string>;
}

type Handler = (request: RecordedRequest) => FakeResponse | Promise<FakeResponse>;

/** メソッドとパスごとに応答を決める。登録していないリクエストは 404 の Problem を返す。 */
export class FakeBackend {
  readonly requests: RecordedRequest[] = [];
  private readonly handlers = new Map<string, Handler>();

  /** `path` はクエリを除いたパス（例 `/tasks/t1/start`）。 */
  on(method: string, path: string, handler: Handler | FakeResponse): this {
    this.handlers.set(`${method} ${path}`, typeof handler === 'function' ? handler : () => handler);
    return this;
  }

  count(method: string, path: string): number {
    return this.requests.filter(
      (request) => request.method === method && request.path.split('?')[0] === path,
    ).length;
  }

  readonly fetch = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const request = input instanceof Request ? input : new Request(input, init);
    const url = new URL(request.url);
    const text = request.body ? await request.text() : '';
    const recorded: RecordedRequest = {
      method: request.method,
      path: `${url.pathname.replace(/^\/api/, '')}${url.search}`,
      headers: request.headers,
      body: text ? (JSON.parse(text) as unknown) : undefined,
    };
    this.requests.push(recorded);
    const handler = this.handlers.get(`${request.method} ${url.pathname.replace(/^\/api/, '')}`);
    const response: FakeResponse = handler
      ? await handler(recorded)
      : { status: 404, body: { type: 'about:blank', status: 404, code: 'not_found' } };
    const status = response.status ?? 200;
    const isProblem = status >= 400;
    return new Response(status === 204 ? null : JSON.stringify(response.body ?? {}), {
      status,
      headers: {
        'Content-Type': isProblem ? 'application/problem+json' : 'application/json',
        ...response.headers,
      },
    });
  };
}

/** 偽の EventSource。テストから `emit` で通知を送る。 */
export class FakeEventSource {
  static instances: FakeEventSource[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: unknown }) => void) | null = null;
  onerror: (() => void) | null = null;
  closed = false;

  constructor(
    readonly url: string,
    readonly authorization?: string,
  ) {
    FakeEventSource.instances.push(this);
  }

  static latest(): FakeEventSource {
    const latest = FakeEventSource.instances.at(-1);
    if (!latest) throw new Error('EventSource が作られていない');
    return latest;
  }

  open(): void {
    this.onopen?.();
  }

  emit(data: unknown): void {
    this.onmessage?.(new MessageEvent('message', { data: JSON.stringify(data) }));
  }

  fail(): void {
    this.onerror?.();
  }

  close(): void {
    this.closed = true;
  }
}

/** ログイン済み（デモ）として扱う AuthService の代わり。 */
export function fakeAuth(): Partial<AuthService> {
  return {
    signedIn: signal(true),
    mode: signal('demo' as const),
    idToken: signal(undefined),
    account: signal({ id: 'demo', email: '', name: '', photoUrl: '' }),
    authorization: () => Promise.resolve(undefined),
  };
}

/** store とコンポーネントのテストに使う provider 一式。 */
export function provideFakeBackend(backend: FakeBackend): Provider[] {
  FakeEventSource.instances = [];
  return [
    { provide: AuthService, useValue: fakeAuth() },
    {
      provide: API_CLIENT,
      useValue: createApiClient({
        origin: 'http://localhost',
        authorization: () => undefined,
        fetch: backend.fetch,
      }),
    },
    {
      provide: EVENT_SOURCE_FACTORY,
      useValue: (url: string, authorization?: string) => new FakeEventSource(url, authorization),
    },
  ];
}

/** 未処理の Promise とタイマーを進める。 */
export async function flush(times = 5): Promise<void> {
  for (let index = 0; index < times; index += 1) {
    await new Promise((resolve) => setTimeout(resolve, 0));
  }
}
