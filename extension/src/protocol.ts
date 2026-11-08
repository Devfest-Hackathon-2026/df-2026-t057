import type { Draft, SleepTime, State, Task, TimePolicy } from '../../shared/models';

export type ExtensionTask = Pick<
  Task,
  'id' | 'title' | 'why' | 'minutes' | 'url' | 'status' | 'plan'
>;
export type ExtensionState = Pick<
  State,
  'revision' | 'balanceSeconds' | 'settings' | 'session' | 'nextDraft' | 'viewingSession'
> & {
  tasks: ExtensionTask[];
  timePolicy?: Pick<TimePolicy, 'version' | 'mode' | 'dailyLimitSeconds' | 'viewedSeconds'>;
  workRewardSeconds: number;
  sleep?: SleepTime;
  timeZone: string;
};

export interface Connection {
  apiBase: string;
  auth: string;
  accountId: string;
}
export interface Snapshot {
  state?: ExtensionState;
  connected: boolean;
  error?: string;
}
export type Moment = 'notice' | 'snooze' | 'complete' | 'balance_zero' | 'night' | 'morning';
export type Request =
  | { type: 'AUTH_SIGN_IN' }
  | { type: 'AUTH_SIGN_OUT' }
  | { type: 'AUTH_RESULT'; credential: unknown }
  | { type: 'DIAGNOSTICS' }
  | { type: 'TEST_NOTICE' }
  | { type: 'CONNECT'; connection: Omit<Connection, 'auth'> & { auth?: string } }
  | { type: 'DISCONNECT'; accountId?: string }
  | { type: 'STATE' }
  | { type: 'START'; taskId: string }
  | { type: 'COMPLETE'; taskId: string }
  | { type: 'PAUSE' }
  | { type: 'DRAFT'; taskId: string; adjust: boolean }
  | { type: 'ADJUST'; draft: Draft; instruction: string }
  | { type: 'COMMIT'; draft: Draft }
  | { type: 'VIEWING'; id: string; seconds: number; sessionId?: string }
  | { type: 'START_VIEWING'; id: string; seconds: number; extendFrom?: string }
  | { type: 'CLAIM_VIEWING'; sessionId: string }
  | { type: 'END_VIEWING'; sessionId: string; reason: 'stop' | 'limit' }
  | { type: 'CONFIRM_WORK'; id: string }
  | { type: 'LINE'; moment: Moment; taskId?: string }
  | { type: 'OPEN_APP'; path?: string }
  | { type: 'CLOSE_TAB' };
export interface Reply {
  diagnostics?: {
    accountId?: string;
    email: string;
    live: boolean;
    notificationPermission: string;
    protocolVersion: number;
  };
  tabId?: number;
  viewingOwnerTabId?: number;
  state?: ExtensionState;
  draft?: Draft;
  line?: string;
  connected?: boolean;
  error?: string;
}
export const MANAGEMENT_ORIGIN = '__MANAGEMENT_ORIGIN__';
export async function send(request: Request): Promise<Reply> {
  return chrome.runtime.sendMessage(request);
}
