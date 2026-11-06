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
  readonly accountEmail = signal('');
  readonly accountName = signal('');
  readonly accountPhoto = signal('');
  readonly authPending = signal(false);
  private auth?: Auth;
  private token = '';
  private accountId = '';
  private sessionVersion = 0;

  async init() {
    try {
      const config = await this.request<Config>('/config');
      this.config.set(config);
      if (config.mode === 'demo') {
        this.signedIn.set(true);
        await this.refresh();
      } else if (config.firebase?.['apiKey']) {
        this.auth = getAuth(initializeApp(config.firebase));
        await new Promise<void>((resolve) => {
          onIdTokenChanged(this.auth!, async (user) => {
            if ((user?.uid ?? '') !== this.accountId) {
              this.accountId = user?.uid ?? '';
              this.sessionVersion++;
              this.token = '';
              this.state.set(null);
              this.error.set('');
              window.postMessage({ type: 'DOPAGAKI_DISCONNECT' }, location.origin);
            }
            const version = this.sessionVersion;
            this.signedIn.set(!!user);
            this.accountEmail.set(user?.email ?? '');
            this.accountName.set(user?.displayName ?? '');
            this.accountPhoto.set(user?.photoURL ?? '');
            this.loading.set(!this.state());
            try {
              const token = user ? await user.getIdToken() : '';
              if (version !== this.sessionVersion) return;
              this.token = token;
              if (user) await this.refresh();
              else {
                this.state.set(null);
                window.postMessage({ type: 'DOPAGAKI_DISCONNECT' }, location.origin);
              }
            } catch (e) {
              if (version === this.sessionVersion) this.error.set(message(e));
            } finally {
              if (version === this.sessionVersion) this.loading.set(false);
              resolve();
            }
          });
        });
      }
    } catch (e) {
      this.error.set(message(e));
    } finally {
      this.loading.set(false);
    }
  }

  async login() {
    if (this.busy()) return false;
    if (!this.auth) {
      this.error.set('FirebaseのGoogleログイン設定が必要です。');
      return false;
    }
    this.authPending.set(true);
    try {
      return await this.run(async () => {
        const provider = new GoogleAuthProvider();
        provider.setCustomParameters({ prompt: 'select_account' });
        await signInWithPopup(this.auth!, provider);
      });
    } finally {
      this.authPending.set(false);
    }
  }
  async logout() {
    if (this.auth) await this.run(() => signOut(this.auth!));
  }
  async request<T>(path: string, method = 'GET', body?: unknown): Promise<T> {
    const version = this.sessionVersion;
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
    if (version !== this.sessionVersion)
      throw new Error('アカウントが切り替わりました。操作をやり直してください。');
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
      {
        type: 'DOPAGAKI_CONNECT',
        apiBase: location.origin,
        auth,
        accountId: this.accountId || auth,
      },
      location.origin,
    );
  }
}
export function message(error: unknown): string {
  if (error && typeof error === 'object' && 'code' in error) {
    if (error.code === 'auth/popup-closed-by-user') return 'ログインをキャンセルしました。';
    if (error.code === 'auth/popup-blocked')
      return 'ポップアップを許可して、もう一度ログインしてください。';
    if (error.code === 'auth/network-request-failed')
      return '接続できませんでした。通信状態を確認して再試行してください。';
  }
  if (error instanceof TypeError)
    return '接続できませんでした。通信状態を確認して再試行してください。';
  return error instanceof Error ? error.message : '操作に失敗しました。再試行してください。';
}
export function localDate(value: string): string {
  const date = new Date(value);
  return new Date(date.getTime() - date.getTimezoneOffset() * 60_000).toISOString().slice(0, 16);
}
