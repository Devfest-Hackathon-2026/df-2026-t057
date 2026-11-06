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
  kind: 'initial' | 'registration' | 'completion' | 'viewing';
  seconds: number;
  taskId?: string;
  at: string;
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
export interface TaskInput {
  id: string;
  title: string;
  notes: string;
  deadline: string;
  minutes: number;
  url: string;
  baseRevision?: number;
}
export interface AssistantCard {
  id: string;
  kind: 'create' | 'task' | 'memory';
  input?: TaskInput;
  taskId?: string;
  preview?: Plan;
  revision?: number;
  memory?: string;
}
export interface AssistantTurn {
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
export function plannedSeconds(task: Task): number {
  return Math.max(60, (Date.parse(task.plan.end) - Date.parse(task.plan.start)) / 1000);
}
