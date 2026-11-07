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
  taskId: string;
  startedAt: string;
  pausedAt?: string;
  pausedSeconds: number;
}
export interface LedgerEntry {
  id: string;
  kind: 'initial' | 'registration' | 'completion' | 'viewing' | 'sleep';
  seconds: number;
  taskId?: string;
  at: string;
  deferred?: boolean;
}
export interface SleepTime {
  bedtime: string;
  wake: string;
}
export interface Sleep extends SleepTime {
  since: string;
  eventId?: string;
  watchedNight?: string;
  settledNight?: string;
  keptNights: number;
}
export interface Morning {
  night: string;
  kept: boolean;
  bonusSeconds: number;
  releasedSeconds: number;
}
export interface State {
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
  sleep?: Sleep;
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
  kind: 'create' | 'task' | 'update' | 'memory' | 'sleep';
  change?: TaskEdit;
  input?: TaskInput;
  taskId?: string;
  preview?: Plan;
  revision?: number;
  memory?: string;
  sleep?: SleepTime;
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
  grime: true,
};
export function elapsedSeconds(session: Session, now = Date.now()): number {
  const end = session.pausedAt ? Date.parse(session.pausedAt) : now;
  return Math.max(
    0,
    Math.floor((end - Date.parse(session.startedAt)) / 1000) - session.pausedSeconds,
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

// The night window in local time: from one hour before bedtime until wake, as on the server.
export function nightWindow(sleep: SleepTime, now = Date.now()) {
  const [bh, bm] = sleep.bedtime.split(':').map(Number);
  const [wh, wm] = sleep.wake.split(':').map(Number);
  for (const back of [0, -1]) {
    const day = new Date(now);
    day.setDate(day.getDate() + back);
    const bed = new Date(
      day.getFullYear(),
      day.getMonth(),
      day.getDate() + (bh < 12 ? 1 : 0),
      bh,
      bm,
    );
    const end = new Date(bed.getFullYear(), bed.getMonth(), bed.getDate(), wh, wm);
    if (end <= bed) end.setDate(end.getDate() + 1);
    const start = bed.getTime() - 3600000;
    const key = `${day.getFullYear()}-${String(day.getMonth() + 1).padStart(2, '0')}-${String(day.getDate()).padStart(2, '0')}`;
    if (now >= start && now < end.getTime()) return { key, start, end: end.getTime() };
  }
  return undefined;
}
