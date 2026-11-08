import { DestroyRef, Injectable, effect, inject, signal, untracked } from '@angular/core';
import type { PageMessage } from '@dopagaki/bridge';
import { AccountScope } from '../auth/account-scope';
import { AuthService } from '../auth/auth.service';
import { API_CLIENT, idempotent, requireData } from '../http/api-client';
import { Operation } from '../http/operation';

/** 認証の値から、拡張がアカウントの切り替えを見分けるための ID を作る（デモには uid がない）。 */
async function demoAccountId(authorization: string): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(authorization));
  return `demo-${Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, '0')).join('')}`;
}

/**
 * 拡張との受け渡し（packages/bridge）。
 * - ログイン中は `DOPAGAKI_CONNECT` を送り、ID トークンが更新されるたびに送り直す。
 * - ログアウト・アカウントの切り替えでは `DOPAGAKI_DISCONNECT` を送る。
 * - 拡張から `DOPAGAKI_EXTENSION_READY` が来たら、接続の情報を送り直す。
 */
@Injectable({ providedIn: 'root' })
export class ExtensionBridge {
  private readonly auth = inject(AuthService);
  private readonly api = inject(API_CLIENT);
  private readonly scope = inject(AccountScope);
  private demoAuthorization: string | undefined;
  private lastConnect: Extract<PageMessage, { type: 'DOPAGAKI_CONNECT' }> | undefined;

  /** 読み込まれている拡張の版。拡張がなければ undefined。 */
  readonly extensionVersion = signal<string | undefined>(undefined);
  /** デモの拡張用トークンの発行。 */
  readonly issueToken = new Operation(this.scope);

  readonly diagnostic = signal<
    { connected: boolean; accountId?: string; notifications: 'granted' | 'denied' } | undefined
  >(undefined);
  readonly diagnosing = signal(false);
  private diagnosticId = '';
  private diagnosticTimer: ReturnType<typeof setTimeout> | undefined;
  diagnose(): void {
    this.diagnostic.set(undefined);
    this.diagnosing.set(true);
    this.diagnosticId = crypto.randomUUID();
    this.post({ type: 'DOPAGAKI_DIAGNOSE', requestId: this.diagnosticId });
    clearTimeout(this.diagnosticTimer);
    this.diagnosticTimer = setTimeout(() => {
      this.diagnosing.set(false);
    }, 3000);
  }
  constructor() {
    this.scope.onReset(() => {
      this.diagnostic.set(undefined);
      this.diagnosticId = '';
      this.diagnosing.set(false);
      clearTimeout(this.diagnosticTimer);
      this.demoAuthorization = undefined;
      this.disconnect();
    });
    effect(() => {
      const signedIn = this.auth.signedIn();
      const token = this.auth.idToken();
      const account = this.auth.account();
      const epoch = this.scope.epoch();
      untracked(() => {
        if (!signedIn || !account) {
          this.disconnect();
          return;
        }
        void this.connect(account.id, token, epoch);
      });
    });
    // 検証に使う zod は大きいので、最初の表示のあとで読み込む。
    const validator = import('@dopagaki/bridge').then((bridge) => bridge.parsePageMessage);
    const listener = async (event: MessageEvent<unknown>): Promise<void> => {
      if (event.source !== window || event.origin !== location.origin) return;
      const message = (await validator)(event.data);
      if (message?.type === 'DOPAGAKI_DIAGNOSTIC' && message.requestId === this.diagnosticId) {
        this.diagnostic.set({
          connected: message.connected,
          ...(message.accountId ? { accountId: message.accountId } : {}),
          notifications: message.notifications,
        });
        this.diagnosing.set(false);
        clearTimeout(this.diagnosticTimer);
        return;
      }
      if (message?.type !== 'DOPAGAKI_EXTENSION_READY') return;
      this.extensionVersion.set(message.version);
      if (this.lastConnect) this.post(this.lastConnect);
    };
    const onMessage = (event: MessageEvent<unknown>): void => {
      void listener(event);
    };
    window.addEventListener('message', onMessage);
    inject(DestroyRef).onDestroy(() => {
      window.removeEventListener('message', onMessage);
      clearTimeout(this.diagnosticTimer);
    });
  }

  private async connect(
    accountId: string,
    idToken: string | undefined,
    epoch: number,
  ): Promise<void> {
    let authorization: string | undefined;
    let id = accountId;
    if (this.auth.mode() === 'demo') {
      authorization = this.demoAuthorization ?? (await this.issueDemoToken());
      if (!authorization) return;
      id = await demoAccountId(authorization);
    } else {
      if (!idToken) return;
      authorization = undefined;
    }
    if (this.scope.epoch() !== epoch) return;
    this.lastConnect = {
      type: 'DOPAGAKI_CONNECT',
      origin: location.origin,
      ...(authorization ? { authorization } : {}),
      accountId: id,
    };
    this.post(this.lastConnect);
  }

  private async issueDemoToken(): Promise<string | undefined> {
    const outcome = await this.issueToken.run(() =>
      requireData(this.api.POST('/extension/token', { params: { header: idempotent() } })),
    );
    if (!outcome.ok) return undefined;
    this.demoAuthorization = outcome.value.authorization;
    return this.demoAuthorization;
  }

  private disconnect(): void {
    if (this.lastConnect)
      this.post({ type: 'DOPAGAKI_DISCONNECT', accountId: this.lastConnect.accountId });
    this.lastConnect = undefined;
  }
  private post(message: PageMessage): void {
    window.postMessage(message, location.origin);
  }
}
