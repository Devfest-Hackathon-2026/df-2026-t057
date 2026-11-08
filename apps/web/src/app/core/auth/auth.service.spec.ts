import { TestBed } from '@angular/core/testing';
import { messages } from '@dopagaki/messages';
import { FakeBackend, flush } from '../../../testing/fake-backend';
import { ConfigStore } from '../config/config.store';
import { AccountScope } from './account-scope';
import { AuthService } from './auth.service';
import { FIREBASE_AUTH_LOADER, type FirebaseAuthPort, type FirebaseUser } from './firebase-auth';

function user(uid: string, token = `${uid}-token`): FirebaseUser {
  return {
    uid,
    email: `${uid}@example.com`,
    displayName: uid,
    photoURL: null,
    getIdToken: () => Promise.resolve(token),
  };
}

class FakeFirebase implements FirebaseAuthPort {
  listener: ((user: FirebaseUser | null) => void) | undefined;
  current: FirebaseUser | null = null;
  signIn = vi.fn(() => Promise.resolve());
  onIdTokenChanged(listener: (user: FirebaseUser | null) => void): void {
    this.listener = listener;
    queueMicrotask(() => {
      listener(this.current);
    });
  }
  currentUser(): FirebaseUser | null {
    return this.current;
  }
  signInWithGoogle(): Promise<void> {
    return this.signIn();
  }
  signOut(): Promise<void> {
    this.change(null);
    return Promise.resolve();
  }
  change(next: FirebaseUser | null): void {
    this.current = next;
    this.listener?.(next);
  }
}

describe('AuthService', () => {
  let firebase: FakeFirebase;

  function setup(config: unknown): AuthService {
    const backend = new FakeBackend().on('GET', '/config', { body: config });
    vi.spyOn(globalThis, 'fetch').mockImplementation(backend.fetch);
    firebase = new FakeFirebase();
    TestBed.configureTestingModule({
      providers: [{ provide: FIREBASE_AUTH_LOADER, useValue: () => Promise.resolve(firebase) }],
    });
    return TestBed.inject(AuthService);
  }

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('デモはログインせずに使える', async () => {
    const auth = setup({ mode: 'demo', features: { ai: false, calendar: false } });
    await auth.start();
    expect(auth.status()).toBe('signedIn');
    expect(await auth.authorization()).toBeUndefined();
  });

  it('設定を読めなければ unavailable にし、やり直せる', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValue(new TypeError('offline'));
    TestBed.configureTestingModule({});
    const auth = TestBed.inject(AuthService);
    await auth.start();
    expect(auth.status()).toBe('unavailable');
    expect(TestBed.inject(ConfigStore).load.error()).toBeDefined();
  });

  it('live で Firebase の設定がなければ、ログインを押したときに知らせる', async () => {
    const auth = setup({ mode: 'live', features: { ai: true, calendar: true } });
    await auth.start();
    expect(auth.status()).toBe('signedOut');
    await auth.login();
    expect(auth.error()).toBe(messages.ja.web.app.auth.firebaseConfigMissing);
  });

  it('ID トークンが更新されると idToken が変わり、アカウントが変わると store を空に戻す', async () => {
    const auth = setup({
      mode: 'live',
      firebase: { apiKey: 'k' },
      features: { ai: true, calendar: true },
    });
    const scope = TestBed.inject(AccountScope);
    firebase.current = user('a');
    await auth.start();
    expect(auth.status()).toBe('signedIn');
    expect(auth.idToken()).toBe('a-token');
    expect(await auth.authorization()).toBe('Bearer a-token');
    const epoch = scope.epoch();

    firebase.change(user('a', 'a-token-2'));
    await flush();
    expect(auth.idToken()).toBe('a-token-2');
    expect(scope.epoch()).toBe(epoch);

    firebase.change(user('b'));
    await flush();
    expect(scope.epoch()).toBe(epoch + 1);
    expect(auth.account()?.id).toBe('b');
    expect(auth.idToken()).toBe('b-token');

    await auth.logout();
    await flush();
    expect(auth.status()).toBe('signedOut');
    expect(scope.epoch()).toBe(epoch + 2);
  });

  it('ポップアップを閉じたら知らせて、ログイン前に戻す', async () => {
    const auth = setup({
      mode: 'live',
      firebase: { apiKey: 'k' },
      features: { ai: true, calendar: true },
    });
    await auth.start();
    firebase.signIn.mockRejectedValueOnce({ code: 'auth/popup-closed-by-user' });
    await auth.login();
    expect(auth.error()).toBe('ログインをキャンセルしました。');
    expect(auth.status()).toBe('signedOut');
  });
});
