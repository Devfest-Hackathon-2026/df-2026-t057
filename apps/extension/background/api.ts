// API の呼び出し。共有のクライアント（@dopagaki/contract）を使い、レスポンスは zod で検証してから返す。
// どの呼び出しも、始めたときと終わったときで接続の版が変わっていたら結果を捨てる（アカウントの切り替え対策）。
import {
  createApiClient,
  etag,
  newIdempotencyKey,
  type ApiClient,
  type CompanionMoment,
  type Draft,
} from '@dopagaki/contract';
import type { z } from 'zod';
import { MANAGEMENT_ORIGIN } from '../lib/config';
import {
  companionLineSchema,
  draftSchema,
  morningSchema,
  profileSchema,
  settingsSchema,
  sleepStateSchema,
  taskListSchema,
  viewingSchema,
  walletSchema,
  workRecordSchema,
  workSessionSchema,
} from '../lib/schemas';
import { connectionVersion, getConnection } from './connection';
import { ExtensionError } from './errors';

const client: ApiClient = createApiClient({
  origin: MANAGEMENT_ORIGIN,
  authorization: async () => (await getConnection())?.authorization,
});

function parse<T>(schema: z.ZodType<T>, value: unknown): T {
  return schema.parse(value);
}

/** 接続していることを確かめてから呼び、終わったときに接続が変わっていないことを確かめる。 */
async function call<T>(request: (api: ApiClient) => Promise<T>): Promise<T> {
  const version = connectionVersion();
  if (!(await getConnection())) throw new ExtensionError('connectFromApp');
  const result = await request(client);
  if (version !== connectionVersion()) throw new ExtensionError('accountChanged');
  return result;
}

const key = () => ({ 'Idempotency-Key': newIdempotencyKey() });

export const api = {
  tasks: () => call(async (c) => parse(taskListSchema, (await c.GET('/tasks')).data)),
  workSession: () =>
    call(async (c) => {
      const data = (await c.GET('/work-session')).data;
      return data?.session ? parse(workSessionSchema, data.session) : null;
    }),
  wallet: () => call(async (c) => parse(walletSchema, (await c.GET('/wallet')).data)),
  viewing: () => call(async (c) => parse(viewingSchema, (await c.GET('/viewing')).data)),
  settings: () => call(async (c) => parse(settingsSchema, (await c.GET('/settings')).data)),
  sleep: () => call(async (c) => parse(sleepStateSchema, (await c.GET('/sleep')).data)),
  profile: () => call(async (c) => parse(profileSchema, (await c.GET('/profile')).data)),

  startTask: (taskId: string) =>
    call(async (c) => {
      const result = await c.POST('/tasks/{taskId}/start', {
        params: { path: { taskId }, header: key() },
      });
      return parse(workSessionSchema, result.data);
    }),
  completeTask: (taskId: string) =>
    call(async (c) => {
      const result = await c.POST('/tasks/{taskId}/complete', {
        params: { path: { taskId }, header: key() },
      });
      const work = result.data?.work;
      return work ? parse(workRecordSchema, work) : undefined;
    }),
  pauseWork: () =>
    call(async (c) =>
      parse(
        workSessionSchema,
        (await c.POST('/work-session/pause', { params: { header: key() } })).data,
      ),
    ),
  resumeWork: () =>
    call(async (c) =>
      parse(
        workSessionSchema,
        (await c.POST('/work-session/resume', { params: { header: key() } })).data,
      ),
    ),
  confirmWork: (sessionId: string) =>
    call(async (c) => {
      const result = await c.POST('/work-session/confirm', {
        // 同じ作業の記録は一度だけ。再送しても同じ結果になるよう、セッションの ID からキーを決める。
        params: { header: { 'Idempotency-Key': `confirm-${sessionId}`.slice(0, 80) } },
        body: { sessionId },
      });
      return parse(workRecordSchema, result.data);
    }),

  draft: (taskId: string, mode: 'snooze' | 'adjust') =>
    call(async (c) => {
      const result = await c.GET('/tasks/{taskId}/reschedule', {
        params: { path: { taskId }, query: { mode } },
      });
      return parse(draftSchema, result.data);
    }),
  adjustDraft: (draft: Draft, instruction: string) =>
    call(async (c) => {
      const result = await c.POST('/reschedule/adjust', {
        params: { header: key() },
        body: { draft, instruction },
      });
      return parse(draftSchema, result.data);
    }),
  commitDraft: (draft: Draft) =>
    call(async (c) => {
      const result = await c.POST('/reschedule/commit', {
        params: { header: { ...key(), 'If-Match': etag(draft.baseRevision) } },
        body: { changes: draft.changes.map(({ taskId, after }) => ({ taskId, after })) },
      });
      return parse(taskListSchema, result.data);
    }),

  /** 視聴の区切りを始める。区切りの ID を Idempotency-Key に使う。 */
  startViewing: (sessionId: string, seconds: 300 | 600, extendFrom: string | undefined) =>
    call(async (c) => {
      const result = await c.POST('/viewing/sessions', {
        params: { header: { 'Idempotency-Key': sessionId } },
        body: extendFrom ? { seconds, extendFrom } : { seconds },
      });
      return parse(viewingSchema, result.data);
    }),
  endViewing: (sessionId: string, reason: 'stop' | 'limit') =>
    call(async (c) => {
      const result = await c.POST('/viewing/sessions/{sessionId}/end', {
        params: {
          path: { sessionId },
          header: { 'Idempotency-Key': `end-${reason}-${sessionId}`.slice(0, 80) },
        },
        body: { reason },
      });
      return parse(viewingSchema, result.data);
    }),
  /** 視聴の報告。受付の ID を Idempotency-Key に使い、再送でも同じ ID を使う。 */
  reportViewing: (receiptId: string, seconds: number, sessionId: string | undefined) =>
    call(async (c) => {
      const result = await c.POST('/viewing/reports', {
        params: { header: { 'Idempotency-Key': receiptId } },
        body: sessionId ? { seconds, sessionId } : { seconds },
      });
      return parse(viewingSchema, result.data);
    }),

  companionLine: (moment: CompanionMoment, taskId: string | undefined) =>
    call(async (c) => {
      const result = await c.POST('/companion/lines', {
        params: { header: key() },
        body: taskId ? { moment, taskId } : { moment },
      });
      return parse(companionLineSchema, result.data).line;
    }),
  settleMorning: () =>
    call(async (c) => {
      const result = await c.POST('/sleep/morning', { params: { header: key() } });
      const morning = result.data?.morning;
      return morning ? parse(morningSchema, morning) : null;
    }),
};

/** 変更通知（SSE）の購読を始める。fetch で Authorization を付ける。 */
export async function openEventStream(signal: AbortSignal): Promise<Response> {
  const connection = await getConnection();
  if (!connection) throw new ExtensionError('connectFromApp');
  return fetch(`${MANAGEMENT_ORIGIN}/api/events`, {
    headers: { Accept: 'text/event-stream', Authorization: connection.authorization },
    cache: 'no-store',
    signal,
  });
}
