import createClient, { type Client, type Middleware } from 'openapi-fetch';
import type { components, paths } from './generated/schema.ts';

export type { paths, components };

type Schemas = components['schemas'];
export type Config = Schemas['Config'];
export type Profile = Schemas['Profile'];
export type ProfileInput = Schemas['ProfileInput'];
export type Settings = Schemas['Settings'];
export type SettingsInput = Schemas['SettingsInput'];
export type NoticeMode = Schemas['NoticeMode'];
export type Task = Schemas['Task'];
export type TaskInput = Schemas['TaskInput'];
export type TaskCreateInput = Schemas['TaskCreateInput'];
export type TaskStatus = Schemas['TaskStatus'];
export type TaskList = Schemas['TaskList'];
export type Plan = Schemas['Plan'];
export type PlanReason = Schemas['PlanReason'];
export type Step = Schemas['Step'];
export type Draft = Schemas['Draft'];
export type PlanChange = Schemas['PlanChange'];
export type PlanChangeInput = Schemas['PlanChangeInput'];
export type WorkSession = Schemas['WorkSession'];
export type WorkRecord = Schemas['WorkRecord'];
export type Wallet = Schemas['Wallet'];
export type TimePolicyMode = Schemas['TimePolicyMode'];
export type TimePolicyInput = Schemas['TimePolicyInput'];
export type LedgerEntry = Schemas['LedgerEntry'];
export type LedgerKind = Schemas['LedgerKind'];
export type Viewing = Schemas['Viewing'];
export type ViewingSession = Schemas['ViewingSession'];
export type Records = Schemas['Records'];
export type Sleep = Schemas['Sleep'];
export type SleepInput = Schemas['SleepInput'];
export type SleepState = Schemas['SleepState'];
export type Morning = Schemas['Morning'];
export type ShortcutStatus = Schemas['ShortcutStatus'];
export type CompanionMoment = Schemas['CompanionMoment'];
export type CompanionLine = Schemas['CompanionLine'];
export type AssistantTurn = Schemas['AssistantTurn'];
export type AssistantCard = Schemas['AssistantCard'];
export type CalendarStatus = Schemas['CalendarStatus'];
export type Resource = Schemas['Resource'];
export type ChangeEvent = Schemas['ChangeEvent'];
export type Problem = Schemas['Problem'];
export type ProblemCode = Schemas['ProblemCode'];
export type FieldProblem = Schemas['FieldProblem'];
export type FieldCode = Schemas['FieldCode'];

/** API が返したエラー（Problem Details）。`code` から表示する文言を決める。 */
export class ApiError extends Error {
  readonly status: number;
  readonly code: ProblemCode;
  readonly fields: readonly FieldProblem[];
  readonly retryAfterSeconds: number | undefined;

  constructor(problem: Problem, retryAfterSeconds?: number) {
    super(problem.code);
    this.name = 'ApiError';
    this.status = problem.status;
    this.code = problem.code;
    this.fields = problem.errors ?? [];
    this.retryAfterSeconds = retryAfterSeconds;
  }
}

/** 通信そのものに失敗した（オフライン、サーバーに届かない）。 */
export class NetworkError extends Error {
  constructor(cause: unknown) {
    super('network', { cause });
    this.name = 'NetworkError';
  }
}

export interface ApiClientOptions {
  /** 例: `https://example.com`。末尾の `/api` は付けない。 */
  readonly origin: string;
  /** `Authorization` ヘッダーの値を返す。無ければ undefined。 */
  readonly authorization: () => Promise<string | undefined> | string | undefined;
  /** テストや拡張の service worker 用に差し替える。 */
  readonly fetch?: typeof fetch;
  readonly credentials?: RequestCredentials;
}

export type ApiClient = Client<paths>;

const MUTATING = new Set(['POST', 'PUT', 'PATCH', 'DELETE']);

/** 再送しても同じ結果になるよう、状態を変えるリクエストに一意のキーを付ける。 */
export function newIdempotencyKey(): string {
  return crypto.randomUUID().replaceAll('-', '');
}

function isProblem(value: unknown): value is Problem {
  return (
    typeof value === 'object' &&
    value !== null &&
    'code' in value &&
    typeof value.code === 'string' &&
    'status' in value &&
    typeof value.status === 'number'
  );
}

export function createApiClient(options: ApiClientOptions): ApiClient {
  const client = createClient<paths>({
    baseUrl: `${options.origin}/api`,
    ...(options.fetch ? { fetch: options.fetch } : {}),
    ...(options.credentials ? { credentials: options.credentials } : {}),
  });

  const headers: Middleware = {
    async onRequest({ request }) {
      const authorization = await options.authorization();
      if (authorization) request.headers.set('Authorization', authorization);
      if (MUTATING.has(request.method) && !request.headers.has('Idempotency-Key')) {
        request.headers.set('Idempotency-Key', newIdempotencyKey());
      }
      return request;
    },
    async onResponse({ response }) {
      if (response.ok || response.status === 303) return response;
      const retryAfter = Number(response.headers.get('Retry-After'));
      const body: unknown = await response
        .clone()
        .json()
        .catch(() => undefined);
      const problem: Problem = isProblem(body)
        ? body
        : { type: 'about:blank', status: response.status, code: 'internal' };
      throw new ApiError(
        problem,
        Number.isFinite(retryAfter) && retryAfter > 0 ? retryAfter : undefined,
      );
    },
    onError({ error }) {
      if (error instanceof ApiError) return error;
      return new NetworkError(error);
    },
  };
  client.use(headers);
  return client;
}

/** `ETag` を `If-Match` に渡す形にする。 */
export function etag(revision: number): string {
  return `"${revision}"`;
}
