import { signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { MATERIAL_ANIMATIONS } from '@angular/material/core';
import { provideRouter } from '@angular/router';
import { AuthService, type Account, type AuthStatus } from '../../core/auth/auth.service';
import { FakeBackend, flush, provideFakeBackend } from '../../../testing/fake-backend';
import { CalendarStore } from '../calendar/calendar.store';
import { AccountMenu } from './account-menu';

const account: Account = {
  id: 'u1',
  email: 'taro@example.com',
  name: '山田 太郎',
  photoUrl: 'https://example.com/photo.png',
};

/** キーボードの操作。Material は `keyCode` で判定するので、それも付ける。 */
function press(target: Element, key: string, keyCode: number): void {
  const event = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true });
  Object.defineProperty(event, 'keyCode', { value: keyCode });
  target.dispatchEvent(event);
}

describe('AccountMenu', () => {
  const status = signal<AuthStatus>('signedIn');
  const current = signal<Account | undefined>(account);
  const login = vi.fn();
  const logout = vi.fn();

  async function render(): Promise<HTMLElement> {
    const fixture = TestBed.createComponent(AccountMenu);
    await flush();
    await fixture.whenStable();
    return fixture.nativeElement as HTMLElement;
  }

  function trigger(element: HTMLElement): HTMLButtonElement {
    const found = element.querySelector<HTMLButtonElement>('button.chip');
    if (!found) throw new Error('trigger not found');
    return found;
  }

  function menu(): HTMLElement | null {
    return document.body.querySelector<HTMLElement>('[role=menu]');
  }

  function items(): HTMLElement[] {
    return Array.from(menu()?.querySelectorAll<HTMLElement>('[role=menuitem]') ?? []);
  }

  async function settle(): Promise<void> {
    await flush();
    TestBed.tick();
  }

  beforeEach(() => {
    status.set('signedIn');
    current.set(account);
    login.mockReset();
    logout.mockReset();
    const backend = new FakeBackend().on('GET', '/calendar', {
      body: { enabled: true, connected: true, sync: 'synced', failedJobs: 0, primaryEvents: false },
    });
    TestBed.configureTestingModule({
      providers: [
        ...provideFakeBackend(backend),
        provideRouter([]),
        { provide: MATERIAL_ANIMATIONS, useValue: { animationsDisabled: true } },
        {
          provide: AuthService,
          useValue: {
            status,
            account: current,
            mode: signal('live' as const),
            signedIn: signal(true),
            idToken: signal(undefined),
            login,
            logout,
          },
        },
      ],
    });
  });

  afterEach(() => {
    document.body.querySelector('.cdk-overlay-container')?.remove();
  });

  it('名前と写真を出し、ボタンの名前にメールアドレスを含める', async () => {
    const element = await render();
    const button = trigger(element);
    expect(button.getAttribute('aria-label')).toBe('アカウントメニュー: taro@example.com');
    expect(button.textContent).toContain('山田 太郎');
    const photo = button.querySelector('img');
    expect(photo?.getAttribute('src')).toBe(account.photoUrl);
    // 名前は文字で出しているので、写真は飾り（空の alt、読み上げない）。
    expect(photo?.getAttribute('alt')).toBe('');
    expect(photo?.closest('[aria-hidden="true"]')).not.toBeNull();
  });

  it('名前がなければメールアドレスを、写真がなければ頭文字を出す', async () => {
    current.set({ ...account, name: '', photoUrl: '' });
    const element = await render();
    const button = trigger(element);
    expect(button.querySelector('img')).toBeNull();
    expect(button.querySelector('.avatar')?.textContent.trim()).toBe('T');
    expect(button.textContent).toContain('taro@example.com');
  });

  it('写真を読み込めなければ頭文字に切り替える', async () => {
    const element = await render();
    trigger(element).querySelector('img')?.dispatchEvent(new Event('error'));
    await settle();
    expect(trigger(element).querySelector('img')).toBeNull();
    expect(trigger(element).querySelector('.avatar')?.textContent.trim()).toBe('山');
  });

  it('メニューにアカウントと連携状態を出し、ログアウトで AuthService を呼ぶ', async () => {
    await TestBed.inject(CalendarStore).refresh();
    const element = await render();
    trigger(element).click();
    await settle();
    const panel = menu();
    expect(panel?.textContent).toContain('taro@example.com');
    expect(panel?.textContent).toContain('こんにちは、山田 太郎さん');
    expect(panel?.textContent).toContain('連携済み');
    const signOut = items().find((item) => item.textContent.includes('ログアウト'));
    if (!signOut) throw new Error('ログアウトがない');
    signOut.click();
    await settle();
    expect(logout).toHaveBeenCalledTimes(1);
    expect(menu()).toBeNull();
  });

  it('キーボードで項目を移動し、Escape で閉じてボタンに戻る', async () => {
    const element = await render();
    const button = trigger(element);
    button.focus();
    press(button, 'Enter', 13);
    button.click();
    await settle();
    const panel = menu();
    if (!panel) throw new Error('メニューが開いていない');
    expect(items().length).toBeGreaterThan(0);
    expect(document.activeElement).toBe(items()[0]);

    press(panel, 'ArrowDown', 40);
    await settle();
    expect(document.activeElement).toBe(items()[1]);
    press(panel, 'ArrowUp', 38);
    await settle();
    expect(document.activeElement).toBe(items()[0]);

    press(panel, 'Escape', 27);
    await settle();
    expect(menu()).toBeNull();
    expect(document.activeElement).toBe(button);
  });

  it('ログイン中は「別のアカウントを使用」を押せない', async () => {
    status.set('signingIn');
    const element = await render();
    expect(trigger(element).textContent).toContain('ログイン中');
    trigger(element).click();
    await settle();
    const switchItem = items().find((item) => item.textContent.includes('別のアカウントを使用'));
    expect(switchItem).toBeInstanceOf(HTMLButtonElement);
    expect((switchItem as HTMLButtonElement).disabled).toBe(true);
  });
});
