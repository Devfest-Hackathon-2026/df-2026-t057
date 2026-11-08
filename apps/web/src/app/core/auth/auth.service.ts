import { Injectable, computed, inject, signal } from '@angular/core';
import { AccountScope } from './account-scope';
import { authErrorMessage } from './auth-errors';
import { FIREBASE_AUTH_LOADER, type FirebaseAuthPort, type FirebaseUser } from './firebase-auth';
import { ConfigStore } from '../config/config.store';
import { LocaleStore } from '../i18n/locale.store';

export type AuthStatus =
  /** 設定とログイン状態を確認している */
  | 'initializing'
  /** 設定を読めなかった */
  | 'unavailable'
  | 'signedOut'
  | 'signingIn'
  | 'signedIn';

export interface Account {
  /** live は Firebase の uid。デモは `demo`。 */
  readonly id: string;
  readonly email: string;
  readonly name: string;
  readonly photoUrl: string;
}

/**
 * ログインの状態（AGENTS.md「管理画面」core/auth）。
 * - live：Firebase の Google ログイン。ID トークンが更新されるたびに `idToken` が変わる。
 * - demo：ログインしない。API は Cookie で利用者を識別する。
 * アカウントが変わったら AccountScope を進め、すべての store を空に戻す。
 */
@Injectable({ providedIn: 'root' })
export class AuthService {
  private readonly configStore = inject(ConfigStore);
  private readonly scope = inject(AccountScope);
  private readonly loadFirebase = inject(FIREBASE_AUTH_LOADER);
  private readonly localeStore = inject(LocaleStore);
  private firebase: FirebaseAuthPort | undefined;
  private started = false;

  readonly status = signal<AuthStatus>('initializing');
  readonly account = signal<Account | undefined>(undefined);
  /** live のときの Firebase ID トークン。更新されると値が変わる。 */
  readonly idToken = signal<string | undefined>(undefined);
  /** ログイン・ログアウトの失敗。 */
  readonly error = signal<string | undefined>(undefined);

  readonly mode = computed(() => this.configStore.config()?.mode);
  readonly signedIn = computed(() => this.status() === 'signedIn');

  async start(): Promise<void> {
    if (this.started) return;
    this.started = true;
    await this.initialize();
  }

  /** 設定の読み込みに失敗したあと、やり直す。 */
  async retry(): Promise<void> {
    if (this.status() !== 'unavailable') return;
    this.status.set('initializing');
    await this.initialize();
  }

  private async initialize(): Promise<void> {
    const loaded = await this.configStore.fetch();
    const config = this.configStore.config();
    if (!loaded || !config) {
      this.status.set('unavailable');
      return;
    }
    if (config.mode === 'demo') {
      this.account.set({ id: 'demo', email: '', name: '', photoUrl: '' });
      this.status.set('signedIn');
      return;
    }
    const firebaseConfig = config.firebase;
    if (!firebaseConfig?.['apiKey']) {
      this.status.set('signedOut');
      return;
    }
    try {
      this.firebase = await this.loadFirebase(firebaseConfig);
    } catch (error: unknown) {
      this.error.set(authErrorMessage(error, this.localeStore.locale()));
      this.status.set('signedOut');
      return;
    }
    await new Promise<void>((resolve) => {
      this.firebase?.onIdTokenChanged((user) => {
        void this.accept(user).finally(resolve);
      });
    });
  }

  private async accept(user: FirebaseUser | null): Promise<void> {
    if (user?.uid !== this.account()?.id) {
      this.idToken.set(undefined);
      this.scope.reset();
    }
    if (!user) {
      this.account.set(undefined);
      if (this.status() !== 'signingIn') this.status.set('signedOut');
      return;
    }
    this.account.set({
      id: user.uid,
      email: user.email ?? '',
      name: user.displayName ?? '',
      photoUrl: user.photoURL ?? '',
    });
    const epoch = this.scope.epoch();
    try {
      const token = await user.getIdToken();
      if (this.scope.epoch() !== epoch) return;
      this.idToken.set(token);
      this.status.set('signedIn');
    } catch (error: unknown) {
      this.error.set(authErrorMessage(error, this.localeStore.locale()));
    }
  }

  /** `Authorization` ヘッダーの値。live は期限が近ければ SDK が更新したトークンを返す。 */
  async authorization(): Promise<string | undefined> {
    const user = this.firebase?.currentUser();
    if (!user) return undefined;
    return `Bearer ${await user.getIdToken()}`;
  }

  /** Google でログインする（別のアカウントへの切り替えにも使う）。 */
  async login(): Promise<void> {
    if (this.status() === 'signingIn') return;
    this.error.set(undefined);
    if (!this.firebase) {
      this.error.set(this.localeStore.web().app.auth.firebaseConfigMissing);
      return;
    }
    const previous = this.status();
    this.status.set('signingIn');
    try {
      await this.firebase.signInWithGoogle();
    } catch (error: unknown) {
      this.error.set(authErrorMessage(error, this.localeStore.locale()));
    } finally {
      // 成功したときは onIdTokenChanged が状態を決める。
      if (this.status() === 'signingIn') {
        this.status.set(
          this.account() ? 'signedIn' : previous === 'signingIn' ? 'signedOut' : previous,
        );
      }
    }
  }

  async logout(): Promise<void> {
    if (!this.firebase) return;
    this.error.set(undefined);
    try {
      await this.firebase.signOut();
    } catch (error: unknown) {
      this.error.set(authErrorMessage(error, this.localeStore.locale()));
    }
  }

  clearError(): void {
    this.error.set(undefined);
  }
}
