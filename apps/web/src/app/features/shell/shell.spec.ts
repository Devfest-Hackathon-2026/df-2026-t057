import { signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { MATERIAL_ANIMATIONS } from '@angular/material/core';
import { Router, provideRouter } from '@angular/router';
import type { Profile } from '@dopagaki/contract';
import { AuthService, type Account } from '../../core/auth/auth.service';
import { ConfigStore } from '../../core/config/config.store';
import { FakeBackend, flush, provideFakeBackend } from '../../../testing/fake-backend';
import { settings, taskList, wallet } from '../../../testing/fixtures';
import { AboutPage } from '../about/about-page';
import { Shell } from './shell';

const profile: Profile = { name: '山田', context: '', timeZone: 'Asia/Tokyo', revision: 1 };
const liveAccount: Account = {
  id: 'u1',
  email: 'taro@example.com',
  name: '山田 太郎',
  photoUrl: '',
};

describe('Shell', () => {
  const mode = signal<'demo' | 'live'>('demo');
  const account = signal<Account | undefined>({ id: 'demo', email: '', name: '', photoUrl: '' });

  async function render(): Promise<HTMLElement> {
    TestBed.inject(ConfigStore).config.set({
      mode: mode(),
      features: { ai: false, calendar: false },
    });
    const fixture = TestBed.createComponent(Shell);
    await flush();
    await fixture.whenStable();
    await flush();
    return fixture.nativeElement as HTMLElement;
  }

  function navLinks(element: HTMLElement): HTMLAnchorElement[] {
    return Array.from(
      element.querySelectorAll<HTMLAnchorElement>('nav[aria-label="メインメニュー"] a'),
    );
  }

  /** リンクの名前（アイコンの文字を除く）。 */
  function linkName(link: HTMLAnchorElement): string {
    return link.querySelector('span')?.textContent.trim() ?? '';
  }

  beforeEach(() => {
    mode.set('demo');
    account.set({ id: 'demo', email: '', name: '', photoUrl: '' });
    // live では初回に案内が自動で開くので、見たことにしておく。
    localStorage.setItem(`dopagaki.tutorial.seen:${liveAccount.email}`, '1');
    const backend = new FakeBackend()
      .on('GET', '/tasks', { body: taskList() })
      .on('GET', '/settings', { body: settings() })
      .on('GET', '/wallet', { body: wallet() })
      .on('GET', '/profile', { body: profile })
      .on('GET', '/work-session', { body: { session: null } })
      .on('GET', '/sleep', { body: { sleep: null, revision: 1 } })
      .on('GET', '/viewing', { body: { session: null } })
      .on('GET', '/calendar', {
        body: { enabled: false, connected: false, sync: 'disconnected', failedJobs: 0 },
      });
    TestBed.configureTestingModule({
      providers: [
        ...provideFakeBackend(backend),
        provideRouter([
          { path: 'calendar', children: [] },
          { path: 'about', component: AboutPage },
        ]),
        { provide: MATERIAL_ANIMATIONS, useValue: { animationsDisabled: true } },
        {
          provide: AuthService,
          useValue: {
            status: signal('signedIn' as const),
            account,
            mode,
            signedIn: signal(true),
            idToken: signal(undefined),
            login: vi.fn(),
            logout: vi.fn(),
            authorization: () => Promise.resolve(undefined),
          },
        },
      ],
    });
  });

  afterEach(() => {
    localStorage.clear();
  });

  it('デモではアカウントメニューを出さず、設定へのリンクとデモの印を出す', async () => {
    const element = await render();
    expect(element.querySelector('app-account-menu')).toBeNull();
    expect(element.querySelector('a[aria-label="設定を開く"]')).not.toBeNull();
    expect(element.textContent).toContain('デモ');
  });

  it('ログインしていればアカウントメニューを出す', async () => {
    mode.set('live');
    account.set(liveAccount);
    const element = await render();
    expect(element.querySelector('app-account-menu')).not.toBeNull();
    expect(element.querySelector('a[aria-label="設定を開く"]')).toBeNull();
    expect(element.textContent).not.toContain('デモ');
  });

  it('開いているページのメニュー項目に aria-current を付ける', async () => {
    const element = await render();
    expect(navLinks(element).map(linkName)).toEqual([
      'カレンダー',
      '課題',
      '記録',
      'アプリについて',
    ]);
    await TestBed.inject(Router).navigateByUrl('/about');
    await flush();
    TestBed.tick();
    const current = navLinks(element).filter(
      (link) => link.getAttribute('aria-current') === 'page',
    );
    expect(current.map(linkName)).toEqual(['アプリについて']);
    expect(element.querySelector('main h1')?.textContent).toContain('このアプリについて');
  });
});
