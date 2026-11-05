import type { Draft, State } from '../../shared/models';

export interface Connection {
  apiBase: string;
  auth: string;
}
export interface Snapshot {
  state?: State;
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
  state?: State;
  draft?: Draft;
  connected?: boolean;
  error?: string;
}
export const MANAGEMENT_ORIGIN = '__MANAGEMENT_ORIGIN__';
export async function send(request: Request): Promise<Reply> {
  return chrome.runtime.sendMessage(request);
}
