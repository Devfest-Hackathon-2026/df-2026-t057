import type { Draft, State, Task } from '../../shared/models';

export type ExtensionTask = Pick<
  Task,
  'id' | 'title' | 'why' | 'minutes' | 'url' | 'status' | 'plan'
>;
export type ExtensionState = Pick<
  State,
  'balanceSeconds' | 'settings' | 'session' | 'nextDraft'
> & {
  tasks: ExtensionTask[];
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
export type Request =
  | { type: 'CONNECT'; connection: Connection }
  | { type: 'DISCONNECT' }
  | { type: 'STATE' }
  | { type: 'START'; taskId: string }
  | { type: 'COMPLETE'; taskId: string }
  | { type: 'PAUSE' }
  | { type: 'DRAFT'; taskId: string; adjust: boolean }
  | { type: 'ADJUST'; draft: Draft; instruction: string }
  | { type: 'COMMIT'; draft: Draft }
  | { type: 'VIEWING'; id: string; seconds: number }
  | { type: 'OPEN_APP'; path?: string };
export interface Reply {
  state?: ExtensionState;
  draft?: Draft;
  connected?: boolean;
  error?: string;
}
export const MANAGEMENT_ORIGIN = '__MANAGEMENT_ORIGIN__';
export async function send(request: Request): Promise<Reply> {
  return chrome.runtime.sendMessage(request);
}
