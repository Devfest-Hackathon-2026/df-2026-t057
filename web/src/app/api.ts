import { Injectable, signal } from '@angular/core';
import { initializeApp } from 'firebase/app';
import {
  getAuth,
  GoogleAuthProvider,
  onIdTokenChanged,
  signInWithPopup,
  signOut,
  Auth,
} from 'firebase/auth';
import { Config, State } from '../../../shared/models';

@Injectable({ providedIn: 'root' })
export class Api {
  readonly config = signal<Config | null>(null);
  readonly state = signal<State | null>(null);
  readonly loading = signal(true);
  readonly busy = signal(false);
  readonly error = signal('');
  readonly signedIn = signal(false);
  private auth?: Auth;
  private token = '';

  async init() {
    try {
      const config = await this.request<Config>('/config');
      this.config.set(config);
      if (config.mode === 'demo') {
        this.signedIn.set(true);
        await this.refresh();
      } else if (config.firebase?.['apiKey']) {
        this.auth = getAuth(initializeApp(config.firebase));
        onIdTokenChanged(this.auth, async (user) => {
          this.signedIn.set(!!user);
          this.token = user ? await user.getIdToken() : '';
          if (user) await this.load();
          else {
            this.state.set(null);
            window.postMessage({ type: 'DOPAGAKI_DISCONNECT' }, location.origin);
          }
        });
      }
    } catch (e) {
      this.error.set(message(e));
    } finally {
      this.loading.set(false);
    }
  }

  async login() {
    if (!this.auth) {
      this.error.set('FirebaseのGoogleログイン設定が必要です。');
      return;
    }
    await this.run(async () => {
      await signInWithPopup(this.auth!, new GoogleAuthProvider());
    });
  }
  async logout() {
    if (this.auth) await signOut(this.auth);
  }
  async request<T>(path: string, method = 'GET', body?: unknown): Promise<T> {
    const response = await fetch(`/api${path}`, {
      method,
      headers: {
        'Content-Type': 'application/json',
        ...(this.token ? { Authorization: `Bearer ${this.token}` } : {}),
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    if (!response.headers.get('Content-Type')?.includes('application/json')) {
      throw new Error('サーバーへ接続できませんでした。少し待って再試行してください。');
    }
    const data = await response.json();
    if (!response.ok)
      throw new Error(data.error ?? '通信に失敗しました。もう一度お試しください。', {
        cause: response.status,
      });
    return data as T;
  }
  async refresh() {
    this.state.set(await this.request<State>('/state'));
    await this.bridge();
  }
  async load() {
    await this.run(() => this.refresh());
  }
  async mutate(path: string, body?: unknown, method = 'POST'): Promise<boolean> {
    return this.run(async () => {
      this.state.set(await this.request<State>(path, method, body));
      await this.bridge();
    });
  }
  async run(action: () => Promise<void>): Promise<boolean> {
    if (this.busy()) return false;
    this.busy.set(true);
    this.error.set('');
    try {
      await action();
      return true;
    } catch (e) {
      this.error.set(message(e));
      return false;
    } finally {
      this.busy.set(false);
    }
  }
  private async bridge() {
    const auth =
      this.config()?.mode === 'demo'
        ? (await this.request<{ token: string }>('/extension/token', 'POST')).token
        : `Bearer ${this.token}`;
    window.postMessage(
      { type: 'DOPAGAKI_CONNECT', apiBase: location.origin, auth },
      location.origin,
    );
  }
}
export function message(error: unknown): string {
  if (error instanceof TypeError)
    return '接続できませんでした。通信状態を確認して再試行してください。';
  return error instanceof Error ? error.message : '操作に失敗しました。再試行してください。';
}
export function localDate(value: string): string {
  const date = new Date(value);
  return new Date(date.getTime() - date.getTimezoneOffset() * 60_000).toISOString().slice(0, 16);
}
