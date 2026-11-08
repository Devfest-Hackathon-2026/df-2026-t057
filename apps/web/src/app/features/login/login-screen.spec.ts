import { signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { AuthService, type AuthStatus } from '../../core/auth/auth.service';
import { LoginScreen } from './login-screen';

describe('LoginScreen', () => {
  const status = signal<AuthStatus>('initializing');
  const error = signal<string | undefined>(undefined);
  const login = vi.fn();

  beforeEach(() => {
    status.set('initializing');
    error.set(undefined);
    TestBed.configureTestingModule({
      providers: [
        {
          provide: AuthService,
          useValue: {
            status,
            error,
            login,
            retry: vi.fn(),
            clearError: () => {
              error.set(undefined);
            },
          },
        },
      ],
    });
  });

  function loginButton(element: HTMLElement): HTMLButtonElement {
    const found = element.querySelector<HTMLButtonElement>('button.login');
    if (!found) throw new Error('login button not found');
    return found;
  }

  it.each([
    ['initializing', '読み込み中…', true],
    ['signedOut', 'Googleでログイン', false],
    ['signingIn', 'ログイン中…', true],
    ['unavailable', 'Googleでログイン', true],
  ] as const)('%s のとき「%s」で、押せないのは %s', async (state, label, disabled) => {
    status.set(state);
    const fixture = TestBed.createComponent(LoginScreen);
    await fixture.whenStable();
    const element = fixture.nativeElement as HTMLElement;
    expect(loginButton(element).textContent.trim()).toBe(label);
    expect(loginButton(element).disabled).toBe(disabled);
    if (state === 'unavailable') {
      expect(element.querySelector('[role=alert]')?.textContent).toContain('再読み込み');
    }
  });

  it('ログインの失敗を知らせ、閉じられる', async () => {
    status.set('signedOut');
    error.set('FirebaseのGoogleログイン設定が必要です。');
    const fixture = TestBed.createComponent(LoginScreen);
    await fixture.whenStable();
    const element = fixture.nativeElement as HTMLElement;
    expect(element.querySelector('[role=alert]')?.textContent).toContain(
      'FirebaseのGoogleログイン設定が必要です。',
    );
    loginButton(element).click();
    expect(login).toHaveBeenCalled();
    Array.from(element.querySelectorAll('button'))
      .find((item) => item.textContent.trim() === '閉じる')
      ?.click();
    await fixture.whenStable();
    expect(element.querySelector('[role=alert]')).toBeNull();
  });

  it('サンプルの完了を試せる', async () => {
    status.set('signedOut');
    const fixture = TestBed.createComponent(LoginScreen);
    await fixture.whenStable();
    const element = fixture.nativeElement as HTMLElement;
    element.querySelector<HTMLButtonElement>('[aria-label="課題完了のサンプルを試す"]')?.click();
    await fixture.whenStable();
    expect(element.querySelector('.reward')?.textContent.trim()).toBe('＋5分');
  });
});
