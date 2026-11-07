export interface Step {
  title: string;
  start: string;
  end: string;
}
export interface Plan {
  start: string;
  end: string;
  steps: Step[];
  reason: string;
}
export interface Task {
  startedAt?: string;
  scheduledStartAt?: string;
  id: string;
  title: string;
  notes: string;
  why?: string;
  deadline: string;
  minutes: number;
  url: string;
  status: 'pending' | 'active' | 'completed';
  createdAt: string;
  completedAt?: string;
  plan: Plan;
  calendarEventId?: string;
}
export interface Profile {
  name: string;
  context: string;
  timeZone: string;
}
export interface Settings {
  snoozeMinutes: number;
  mode: 'pachinko' | 'mahjong' | 'simple';
  sound: boolean;
  reducedMotion: boolean;
  grime: boolean;
}
export interface Session {
  id?: string;
  limitSeconds?: number;
  taskId: string;
  startedAt: string;
  pausedAt?: string;
  pausedSeconds: number;
}
export interface LedgerEntry {
  id: string;
  kind: 'initial' | 'registration' | 'completion' | 'viewing' | 'daily' | 'work';
  workSeconds?: number;
  seconds: number;
  taskId?: string;
  at: string;
}
export interface State {
  timePolicy?: TimePolicy;
  viewingSession?: ViewingSession;
  viewingHistory?: ViewingSession[];
  revision: number;
  planRevision: number;
  profile: Profile;
  settings: Settings;
  balanceSeconds: number;
  tasks: Task[];
  session?: Session;
  ledger: LedgerEntry[];
  calendarConnected: boolean;
  calendarSync: string;
  lastSync?: string;
  nextDraft?: Draft;
  memoryReceipts?: Record<string, boolean>;
}
export interface TimePolicy {
  version: number;
  mode: 'rewards' | 'fixed' | 'reminder' | 'self';
  dailyLimitSeconds: number;
  timeZone: string;
  day: string;
  dayStartedAt: string;
  resetAt: string;
  viewedSeconds: number;
  rewardSeconds: number;
  workSeconds: number;
  history: { day: string; viewedSeconds: number; workSeconds: number }[];
}
export interface ViewingSession {
  extendFrom?: string;
  id: string;
  startedAt: string;
  plannedSeconds: number;
  consumedSeconds: number;
  lastReportAt: string;
  endedAt?: string;
  reason?: 'stop' | 'limit';
}
export function viewingAvailable(state: {
  balanceSeconds: number;
  timePolicy?: Pick<TimePolicy, 'mode' | 'dailyLimitSeconds' | 'viewedSeconds'>;
}): number {
  const p = state.timePolicy;
  if (!p) return state.balanceSeconds;
  if (p.mode === 'reminder' || p.mode === 'self') return 600;
  return Math.max(0, Math.min(state.balanceSeconds, p.dailyLimitSeconds - p.viewedSeconds));
}
export function workLimitSeconds(state: Pick<State, 'session'>, task: Pick<Task, 'plan'>): number {
  return state.session?.limitSeconds ?? plannedSeconds(task);
}
export function workRewardText(before: State, after: State): string {
  if (!after.timePolicy) return '課題完了！ 視聴時間 ＋20分';
  const ids = new Set(before.ledger.map((e) => e.id));
  const seconds = after.ledger
    .filter((e) => e.kind === 'work' && !ids.has(e.id))
    .reduce((sum, e) => sum + e.seconds, 0);
  return workRewardMessage(seconds);
}
export function workRewardMessage(seconds: number, legacy = false): string {
  if (legacy) return '課題完了！ 視聴時間 ＋20分';
  return seconds
    ? `作業を記録しました。視聴時間 ＋${durationLabel(seconds)}`
    : '作業を記録しました';
}
export function durationLabel(seconds: number): string {
  const minutes = Math.floor(seconds / 60),
    rest = seconds % 60;
  return `${minutes ? `${minutes}分` : ''}${rest || !minutes ? `${rest}秒` : ''}`;
}
export function rewardDescription(state?: { timePolicy?: Pick<TimePolicy, 'mode'> }): string {
  if (!state?.timePolicy) return '完了で視聴時間 ＋20分';
  return state.timePolicy.mode === 'rewards'
    ? '取り組んだ5分で視聴1分。記録した分だけ増えます'
    : '区切りまで、ひとつずつ取り組みましょう';
}
export interface PlanChange {
  taskId: string;
  title: string;
  before: Plan;
  after: Plan;
  originalMinutes: number;
  plannedMinutes: number;
}
export interface Draft {
  id: string;
  taskId: string;
  baseRevision: number;
  createdAt: string;
  start: string;
  changes: PlanChange[];
  reason: string;
}
export interface Config {
  mode: 'demo' | 'live';
  aiEnabled: boolean;
  calendarEnabled: boolean;
  projectId: string;
  firebase?: Record<string, string>;
}
export interface TaskEdit {
  input: TaskInput;
  draft: Draft;
}
export interface TaskInput {
  id: string;
  title: string;
  notes: string;
  why?: string;
  deadline: string;
  minutes: number;
  url: string;
  baseRevision?: number;
}
export interface AssistantCard {
  id: string;
  kind: 'create' | 'task' | 'update' | 'memory';
  change?: TaskEdit;
  input?: TaskInput;
  taskId?: string;
  preview?: Plan;
  revision?: number;
  memory?: string;
}
export interface AssistantTurn {
  taskId?: string;
  id: string;
  text: string;
  reply: string;
  cards: AssistantCard[];
  status: 'pending' | 'ready' | 'error';
  failure?: string;
  createdAt: string;
  pendingUntil?: string;
}
export const DEFAULT_SETTINGS: Settings = {
  snoozeMinutes: 10,
  mode: 'pachinko',
  sound: false,
  reducedMotion: false,
  grime: false,
};
export function elapsedSeconds(session: Session, now = Date.now()): number {
  const end = session.pausedAt ? Date.parse(session.pausedAt) : now;
  return Math.min(
    session.limitSeconds ?? Infinity,
    Math.max(0, Math.floor((end - Date.parse(session.startedAt)) / 1000) - session.pausedSeconds),
  );
}
export function displayTime(seconds: number): string {
  const value = Math.max(0, Math.floor(seconds));
  return `${Math.floor(value / 60)
    .toString()
    .padStart(2, '0')}:${(value % 60).toString().padStart(2, '0')}`;
}
export function plannedSeconds(task: Pick<Task, 'plan'>): number {
  return Math.max(60, (Date.parse(task.plan.end) - Date.parse(task.plan.start)) / 1000);
}
