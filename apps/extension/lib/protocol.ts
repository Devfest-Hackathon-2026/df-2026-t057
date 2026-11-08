// 拡張の中（content script・ポップアップ ⇄ background）で受け渡すメッセージ。
// すべて判別可能な union にし、受け取った側が zod で検証してから使う（AGENTS.md「Chrome拡張」）。
import { z } from 'zod';
import {
  companionMoments,
  draftSchema,
  extensionStateSchema,
  extensionTaskSchema,
  idSchema,
  taskCreateInputSchema,
  workRecordSchema,
} from './schemas';

/** 再送しても同じ結果になるよう、content script が決めて送るキー（Idempotency-Key に使う）。 */
export const clientKeySchema = z.string().regex(/^[A-Za-z0-9_-]{8,80}$/);

// ── content script・ポップアップ → background（runtime.sendMessage） ──
export const requestSchema = z.discriminatedUnion('type', [
  z.object({
    type: z.literal('CONNECT'),
    origin: z.url(),
    authorization: z.string().min(1).max(4096).exactOptional(),
    accountId: z.string().min(1).max(128),
  }),
  z.object({ type: z.literal('DISCONNECT'), accountId: z.string().optional() }),
  z.object({ type: z.literal('SIGN_IN') }),
  z.object({ type: z.literal('AUTH_RESULT'), credential: z.unknown() }),
  z.object({ type: z.literal('TEST_NOTICE') }),
  z.object({ type: z.literal('DIAGNOSE') }),
  z.object({
    type: z.literal('GET_STATE'),
    /** true なら API から取り直す（再接続のボタンなど）。false なら変更通知で最新なら手元の状態を返す。 */
    refresh: z.boolean(),
  }),
  z.object({ type: z.literal('CREATE_TASK'), input: taskCreateInputSchema }),
  z.object({ type: z.literal('START_TASK'), taskId: idSchema }),
  z.object({ type: z.literal('COMPLETE_TASK'), taskId: idSchema }),
  z.object({ type: z.literal('PAUSE_WORK') }),
  z.object({ type: z.literal('RESUME_WORK') }),
  z.object({ type: z.literal('CONFIRM_WORK'), sessionId: idSchema }),
  z.object({
    type: z.literal('GET_DRAFT'),
    taskId: idSchema,
    mode: z.enum(['snooze', 'adjust']),
  }),
  z.object({
    type: z.literal('ADJUST_DRAFT'),
    draft: draftSchema,
    instruction: z.string().trim().min(1).max(2000),
  }),
  z.object({ type: z.literal('COMMIT_DRAFT'), draft: draftSchema }),
  z.object({
    type: z.literal('START_VIEWING'),
    sessionId: clientKeySchema,
    seconds: z.union([z.literal(300), z.literal(600)]),
    extendFrom: idSchema.exactOptional(),
  }),
  z.object({ type: z.literal('CLAIM_VIEWING'), sessionId: idSchema }),
  z.object({
    type: z.literal('END_VIEWING'),
    sessionId: idSchema,
    reason: z.enum(['stop', 'limit']),
  }),
  z.object({
    type: z.literal('REPORT_VIEWING'),
    receiptId: clientKeySchema,
    seconds: z.number().int().min(1).max(65),
    sessionId: idSchema.exactOptional(),
  }),
  z.object({
    type: z.literal('COMPANION_LINE'),
    moment: z.enum(companionMoments),
    taskId: idSchema.exactOptional(),
  }),
  z.object({ type: z.literal('OPEN_APP') }),
  z.object({ type: z.literal('CLOSE_TAB') }),
]);
export type Request = z.infer<typeof requestSchema>;
export type RequestType = Request['type'];

export const replySchema = z.discriminatedUnion('ok', [
  z.object({
    ok: z.literal(true),
    connected: z.boolean(),
    accountId: z.string().optional(),
    notifications: z.enum(['granted', 'denied']).optional(),
    state: extensionStateSchema.exactOptional(),
    /** 送ったタブが視聴の区切りの持ち主か。 */
    ownsViewing: z.boolean().exactOptional(),
    draft: draftSchema.exactOptional(),
    line: z.string().max(200).exactOptional(),
    work: workRecordSchema.exactOptional(),
  }),
  z.object({
    ok: z.literal(false),
    /** 利用者に見せる一文（packages/messages から引いたもの）。 */
    error: z.string().max(500),
    /** API の Problem Details の code。 */
    code: z.string().max(80).exactOptional(),
    /** 同じ内容で送り直せば通る見込みがあるか（通信の失敗など）。false なら送り直さない。 */
    retryable: z.boolean(),
  }),
]);
export type Reply = z.infer<typeof replySchema>;
export type OkReply = Extract<Reply, { ok: true }>;

// ── background → content script（runtime.Port） ──
export const PORT_NAME = 'dopagaki';

export const portPushSchema = z.discriminatedUnion('type', [
  z.object({
    type: z.literal('STATE'),
    /** null は未接続（ログアウト・アカウントの切り替え）。content script は表示を消す。 */
    state: extensionStateSchema.nullable(),
    ownsViewing: z.boolean(),
  }),
]);
export type PortPush = z.infer<typeof portPushSchema>;

/** content script → background。service worker を止めないための合図。 */
export const portPingSchema = z.object({ type: z.literal('PING') });

// ── background → content script（tabs.sendMessage。表示したかを返してもらう） ──
export const tabCommandSchema = z.discriminatedUnion('type', [
  z.object({
    type: z.literal('SHOW_NOTICE'),
    task: extensionTaskSchema,
    state: extensionStateSchema,
    line: z.string().max(200),
  }),
  z.object({
    type: z.literal('SHOW_NIGHT'),
    state: extensionStateSchema,
    line: z.string().max(200),
  }),
]);
export type TabCommand = z.infer<typeof tabCommandSchema>;

export const shownReplySchema = z.object({ shown: z.boolean() });
