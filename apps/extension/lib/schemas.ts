// API から受け取った値と、拡張の中で受け渡す値の zod スキーマ。
// API のレスポンスも信頼できない入力として扱い、画面に出す前に必ずここで検証する（AGENTS.md「境界では必ず検証する」）。
// 型は仕様から生成した型（@dopagaki/contract）に代入できることを `z.ZodType<…>` で確かめる。
import type {
  CompanionMoment,
  Draft,
  Morning,
  NoticeMode,
  Plan,
  PlanChange,
  PlanReason,
  Resource,
  Settings,
  Step,
  TaskStatus,
  TimePolicyMode,
  Viewing,
  ViewingSession,
  WorkRecord,
  WorkSession,
} from '@dopagaki/contract';
import { z } from 'zod';

export const idSchema = z.string().regex(/^[A-Za-z0-9_-]{1,80}$/);

/** RFC 3339 の日時。Date.parse で読めない値は受け付けない。 */
export const instantSchema = z
  .string()
  .max(64)
  .refine((value) => !Number.isNaN(Date.parse(value)), 'invalid date-time');

const seconds = z.number().int().min(0).max(10_000_000);
const signedSeconds = z.number().int().min(-10_000_000).max(10_000_000);

const planReasons = [
  'backward',
  'ai_detailed',
  'ai_limit_fallback',
  'snooze_kept',
  'snooze_shortened',
  'edit_replanned',
  'ai_adjusted',
  'manual',
] as const satisfies readonly PlanReason[];

export const noticeModes = [
  'pachinko',
  'mahjong',
  'simple',
] as const satisfies readonly NoticeMode[];
const taskStatuses = ['pending', 'active', 'completed'] as const satisfies readonly TaskStatus[];
const policyModes = [
  'rewards',
  'fixed',
  'reminder',
  'self',
] as const satisfies readonly TimePolicyMode[];
export const companionMoments = [
  'notice',
  'snooze',
  'complete',
  'balance_zero',
  'app_open',
  'night',
  'morning',
] as const satisfies readonly CompanionMoment[];
export const resources = [
  'profile',
  'settings',
  'tasks',
  'workSession',
  'wallet',
  'viewing',
  'sleep',
  'shortcut',
  'assistant',
  'calendar',
] as const satisfies readonly Resource[];

export const stepSchema: z.ZodType<Step> = z.object({
  title: z.string().min(1).max(200),
  start: instantSchema,
  end: instantSchema,
});

export const planSchema: z.ZodType<Plan> = z.object({
  start: instantSchema,
  end: instantSchema,
  steps: z.array(stepSchema).min(1).max(20),
  reason: z.enum(planReasons),
  note: z.string().max(500).exactOptional(),
});

/** 拡張が使う課題の項目だけ（メモや締切は持たない）。 */
export const extensionTaskSchema = z.object({
  id: idSchema,
  title: z.string().max(200),
  why: z.string().max(200),
  minutes: z.number().int().min(0).max(1440),
  url: z.string().max(2000),
  status: z.enum(taskStatuses),
  plan: planSchema,
});
export type ExtensionTask = z.infer<typeof extensionTaskSchema>;

export const planChangeSchema: z.ZodType<PlanChange> = z.object({
  taskId: idSchema,
  title: z.string().max(200),
  before: planSchema,
  after: planSchema,
  originalMinutes: z.number().int().min(0).max(1440),
  plannedSeconds: seconds,
});

export const draftSchema: z.ZodType<Draft> = z.object({
  id: idSchema,
  kind: z.enum(['snooze', 'adjust', 'edit']),
  taskId: idSchema,
  baseRevision: z.number().int().min(0),
  createdAt: instantSchema,
  start: instantSchema.exactOptional(),
  shortened: z.boolean(),
  changes: z.array(planChangeSchema).max(200),
});

export const taskListSchema = z.object({
  items: z.array(extensionTaskSchema).max(500),
  revision: z.number().int().min(0),
  nextSnooze: draftSchema.nullable(),
});

export const workSessionSchema: z.ZodType<WorkSession> = z.object({
  id: idSchema,
  taskId: idSchema,
  startedAt: instantSchema,
  limitSeconds: seconds,
  elapsedSeconds: seconds,
  running: z.boolean(),
  asOf: instantSchema,
  pausedAt: instantSchema.exactOptional(),
});

/** 作業の記録。報酬の表示に使う。 */
export const workRecordSchema: z.ZodType<WorkRecord> = z.object({
  sessionId: idSchema,
  taskId: idSchema,
  workSeconds: seconds,
  rewardSeconds: seconds,
  deferred: z.boolean(),
});

export const viewingSessionSchema: z.ZodType<ViewingSession> = z.object({
  id: idSchema,
  extendFrom: idSchema.exactOptional(),
  startedAt: instantSchema,
  plannedSeconds: seconds,
  consumedSeconds: seconds,
  endedAt: instantSchema.exactOptional(),
  endReason: z.enum(['stop', 'limit']).exactOptional(),
});

export const viewingSchema: z.ZodType<Viewing> = z.object({
  session: viewingSessionSchema.nullable(),
  availableSeconds: signedSeconds,
  balanceSeconds: signedSeconds,
  requiresSession: z.boolean(),
});

export const settingsSchema: z.ZodType<Settings> = z.object({
  snoozeMinutes: z.number().int().min(1).max(120),
  mode: z.enum(noticeModes),
  sound: z.boolean(),
  reducedMotion: z.boolean(),
  grime: z.boolean(),
  revision: z.number().int().min(0),
});

/** API の Wallet から、拡張が使う項目だけを取り出したもの。 */
export const walletSchema = z.object({
  revision: z.number().int().min(0),
  balanceSeconds: signedSeconds,
  availableSeconds: signedSeconds,
  deferredSeconds: seconds,
  policy: z.object({
    mode: z.enum(policyModes),
    dailyLimitSeconds: seconds,
    viewedSeconds: seconds,
  }),
  rules: z.object({
    workSecondsPerReward: z.number().int().min(1),
    rewardSecondsPerUnit: z.number().int().min(0),
  }),
});
export type ExtensionWallet = z.infer<typeof walletSchema>;

export const sleepSchema = z.object({
  bedtime: z.string().regex(/^([01][0-9]|2[0-3]):[0-5][0-9]$/),
  wake: z.string().regex(/^([01][0-9]|2[0-3]):[0-5][0-9]$/),
  night: z.object({ start: instantSchema, end: instantSchema, active: z.boolean() }),
});
export type ExtensionSleep = z.infer<typeof sleepSchema>;

export const sleepStateSchema = z.object({
  revision: z.number().int().min(0),
  sleep: sleepSchema.nullable(),
});

export const morningSchema: z.ZodType<Morning> = z.object({
  night: z.string().max(32),
  kept: z.boolean(),
  bonusSeconds: seconds,
  releasedSeconds: seconds,
});

export const companionLineSchema = z.object({ line: z.string().max(200) });

export const changeEventSchema = z.object({
  resource: z.enum(resources),
  revision: z.number().int().min(0),
});

/**
 * 拡張が保持する状態。content script とポップアップへはこの形で渡す。
 * プロフィールや台帳は持たない（必要な項目だけを残す）。
 * `workSyncedAt` はタイマーを数え上げる基準にする、作業の状態を受け取った時点の端末の時刻（ミリ秒）。
 */
export const extensionStateSchema = z.object({
  tasks: z.array(extensionTaskSchema).max(500),
  planRevision: z.number().int().min(0),
  nextSnooze: draftSchema.nullable(),
  workSession: workSessionSchema.nullable(),
  workSyncedAt: z.number().int().min(0),
  wallet: walletSchema.nullable(),
  viewing: viewingSchema.nullable(),
  settings: settingsSchema.nullable(),
  sleep: sleepSchema.nullable(),
});
export type ExtensionState = z.infer<typeof extensionStateSchema>;

export const emptyState: ExtensionState = {
  tasks: [],
  planRevision: 0,
  nextSnooze: null,
  workSession: null,
  workSyncedAt: 0,
  wallet: null,
  viewing: null,
  settings: null,
  sleep: null,
};

/** 設定を読み込む前に使う、表示の既定値。 */
export const defaultSettings: Settings = {
  snoozeMinutes: 10,
  mode: 'pachinko',
  sound: false,
  reducedMotion: false,
  grime: false,
  revision: 0,
};
