import {
  DestroyRef,
  Injectable,
  InjectionToken,
  effect,
  inject,
  signal,
  untracked,
} from '@angular/core';
import { Subject } from 'rxjs';
import type { ChangeEvent, Resource } from '@dopagaki/contract';
import { AccountScope } from '../auth/account-scope';
import { AuthService } from '../auth/auth.service';

/** EventSource の生成。テストでは偽物に差し替える。 */
export type EventSourceFactory = (url: string) => EventSource;

export const EVENT_SOURCE_FACTORY = new InjectionToken<EventSourceFactory>('EventSourceFactory', {
  providedIn: 'root',
  factory: () => (url) => new EventSource(url, { withCredentials: true }),
});

const resources: readonly Resource[] = [
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
];

/** 受け取った data を検証する（境界では必ず検証する）。 */
export function parseChangeEvent(data: unknown): ChangeEvent | undefined {
  if (typeof data !== 'string') return undefined;
  let value: unknown;
  try {
    value = JSON.parse(data);
  } catch {
    return undefined;
  }
  if (typeof value !== 'object' || value === null) return undefined;
  const resource: unknown = 'resource' in value ? value.resource : undefined;
  const revision: unknown = 'revision' in value ? value.revision : undefined;
  if (typeof resource !== 'string' || !resources.includes(resource as Resource)) return undefined;
  if (typeof revision !== 'number' || !Number.isInteger(revision)) return undefined;
  return { resource: resource as Resource, revision };
}

const minRetryMs = 1000;
const maxRetryMs = 60_000;

/** 失敗した回数から、次に接続し直すまでの時間を決める（指数バックオフ）。 */
export function retryDelayMs(attempt: number, random: number = Math.random()): number {
  const base = Math.min(maxRetryMs, minRetryMs * 2 ** attempt);
  // 一斉に接続し直さないよう、最大で2割ずらす。
  return Math.round(base * (0.8 + random * 0.2));
}

export type RealtimeState = 'idle' | 'connecting' | 'open' | 'retrying';

/**
 * `GET /api/events` の変更通知（AGENTS.md「リアルタイム同期」）。
 * データ本体は来ないので、store は通知の版が自分の持つ版より新しければ取り直す。
 * 切れていた間の変更に備え、つなぎ直したら `reconnected$` で全部を取り直させる。
 */
@Injectable({ providedIn: 'root' })
export class RealtimeService {
  private readonly auth = inject(AuthService);
  private readonly scope = inject(AccountScope);
  private readonly createSource = inject(EVENT_SOURCE_FACTORY);
  private readonly changes = new Subject<ChangeEvent>();
  private readonly reconnects = new Subject<void>();
  private readonly latest = new Map<Resource, number>();
  private source: EventSource | undefined;
  private retryTimer: ReturnType<typeof setTimeout> | undefined;
  private attempt = 0;
  /** 今のアカウントで一度でも接続できたか。2回目以降の接続は「つなぎ直し」。 */
  private connectedOnce = false;
  private url: string | undefined;

  readonly changes$ = this.changes.asObservable();
  readonly reconnected$ = this.reconnects.asObservable();
  readonly state = signal<RealtimeState>('idle');

  constructor() {
    this.scope.onReset(() => {
      this.latest.clear();
      this.connectedOnce = false;
    });
    effect(() => {
      const url = this.targetUrl();
      untracked(() => {
        this.retarget(url);
      });
    });
    inject(DestroyRef).onDestroy(() => {
      this.retarget(undefined);
    });
  }

  /** そのリソースについて、これまでに受け取った一番新しい版。 */
  latestRevision(resource: Resource): number | undefined {
    return this.latest.get(resource);
  }

  private targetUrl(): string | undefined {
    if (!this.auth.signedIn()) return undefined;
    if (this.auth.mode() === 'demo') return '/api/events';
    const token = this.auth.idToken();
    // ID トークンが更新されたら、新しいトークンでつなぎ直す。
    return token ? `/api/events?token=${encodeURIComponent(token)}` : undefined;
  }

  private retarget(url: string | undefined): void {
    if (url === this.url) return;
    this.url = url;
    this.close();
    this.attempt = 0;
    if (url) this.open(url);
    else this.state.set('idle');
  }

  private open(url: string): void {
    this.state.set('connecting');
    const source = this.createSource(url);
    this.source = source;
    source.onopen = () => {
      if (this.source !== source) return;
      this.attempt = 0;
      this.state.set('open');
      if (this.connectedOnce) this.reconnects.next();
      this.connectedOnce = true;
    };
    source.onmessage = (message: MessageEvent<unknown>) => {
      if (this.source !== source) return;
      const event = parseChangeEvent(message.data);
      if (!event) return;
      const known = this.latest.get(event.resource);
      if (known === undefined || event.revision > known)
        this.latest.set(event.resource, event.revision);
      this.changes.next(event);
    };
    source.onerror = () => {
      if (this.source !== source) return;
      // EventSource 自身の再接続に任せず、間隔を空けてつなぎ直す（401 のときは自動で再接続しないため）。
      this.close();
      this.state.set('retrying');
      const delay = retryDelayMs(this.attempt);
      this.attempt += 1;
      this.retryTimer = setTimeout(() => {
        this.retryTimer = undefined;
        if (this.url) this.open(this.url);
      }, delay);
    };
  }

  private close(): void {
    if (this.retryTimer !== undefined) clearTimeout(this.retryTimer);
    this.retryTimer = undefined;
    this.source?.close();
    this.source = undefined;
  }
}
