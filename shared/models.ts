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
  calendarPending?: boolean;
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

// Wall-clock parts of an instant in a time zone; an unknown zone falls back to the browser's own.
function zoned(timeZone: string, at: number) {
  let format: Intl.DateTimeFormat;
  try {
    format = new Intl.DateTimeFormat('en-US', {
      timeZone,
      hourCycle: 'h23',
      year: 'numeric',
      month: 'numeric',
      day: 'numeric',
      hour: 'numeric',
      minute: 'numeric',
    });
  } catch {
    format = new Intl.DateTimeFormat('en-US', {
      hourCycle: 'h23',
      year: 'numeric',
      month: 'numeric',
      day: 'numeric',
      hour: 'numeric',
      minute: 'numeric',
    });
  }
  const parts = Object.fromEntries(format.formatToParts(at).map((p) => [p.type, Number(p.value)]));
  return {
    y: parts['year'],
    m: parts['month'],
    d: parts['day'],
    h: parts['hour'],
    mi: parts['minute'],
  };
}
// The instant at which the wall clock in timeZone reads the given time. Overflowing days roll over.
function instant(timeZone: string, y: number, m: number, d: number, h: number, mi: number) {
  const wall = Date.UTC(y, m - 1, d, h, mi);
  let guess = wall;
  for (let i = 0; i < 2; i++) {
    const shown = zoned(timeZone, guess);
    guess += wall - Date.UTC(shown.y, shown.m - 1, shown.d, shown.h, shown.mi);
  }
  return guess;
}
// The night window in the profile's time zone, as on the server: one hour before bedtime until wake.
export function nightWindow(sleep: SleepTime, timeZone: string, now = Date.now()) {
  const [bh, bm] = sleep.bedtime.split(':').map(Number);
  const [wh, wm] = sleep.wake.split(':').map(Number);
  const today = zoned(timeZone, now);
  for (const back of [0, -1]) {
    const day = new Date(Date.UTC(today.y, today.m - 1, today.d + back));
    const [y, m, d] = [day.getUTCFullYear(), day.getUTCMonth() + 1, day.getUTCDate()];
    const bedDay = d + (bh < 12 ? 1 : 0);
    const bed = instant(timeZone, y, m, bedDay, bh, bm);
    let end = instant(timeZone, y, m, bedDay, wh, wm);
    if (end <= bed) end = instant(timeZone, y, m, bedDay + 1, wh, wm);
    const start = bed - 3600000;
    const key = `${y}-${String(m).padStart(2, '0')}-${String(d).padStart(2, '0')}`;
    if (now >= start && now < end) return { key, start, end };
  }
  return undefined;
}
