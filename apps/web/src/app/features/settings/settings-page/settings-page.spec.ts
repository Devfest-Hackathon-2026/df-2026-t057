import { TestbedHarnessEnvironment } from '@angular/cdk/testing/testbed';
import { TestBed, type ComponentFixture } from '@angular/core/testing';
import { MATERIAL_ANIMATIONS } from '@angular/material/core';
import { MatSelectHarness } from '@angular/material/select/testing';
import type { CalendarStatus, Profile, SleepState } from '@dopagaki/contract';
import { messages } from '@dopagaki/messages';
import { ConfigStore } from '../../../core/config/config.store';
import { LocaleStore } from '../../../core/i18n/locale.store';
import { FakeBackend, flush, provideFakeBackend } from '../../../../testing/fake-backend';
import { settings, taskList, wallet, workSession } from '../../../../testing/fixtures';
import { CalendarStore } from '../../calendar/calendar.store';
import { ShortcutStore } from '../../shortcut/shortcut.store';
import { SleepStore } from '../../sleep/sleep.store';
import { WalletStore } from '../../wallet/wallet.store';
import { WorkSessionStore } from '../../work/work-session.store';
import { ProfileStore } from '../profile.store';
import { SettingsStore } from '../settings.store';
import { SettingsPage } from './settings-page';

const profile: Profile = {
  name: '山田',
  context: '',
  timeZone: 'Asia/Tokyo',
  locale: 'ja',
  revision: 1,
};
const noSleep: SleepState = { sleep: null, revision: 1 };
const connectedCalendar: CalendarStatus = {
  enabled: true,
  connected: true,
  sync: 'synced',
  failedJobs: 0,
  lastSync: '2026-11-08T01:00:00Z',
  primaryEvents: true,
};
const disconnectedCalendar: CalendarStatus = {
  enabled: true,
  connected: false,
  sync: 'disconnected',
  failedJobs: 0,
  primaryEvents: false,
};
const problem = (status: number, code: string) => ({
  status,
  body: { type: 'about:blank', status, code },
});

describe('SettingsPage', () => {
  let backend: FakeBackend;
  let fixture: ComponentFixture<SettingsPage>;
  let element: HTMLElement;

  async function render(): Promise<void> {
    TestBed.inject(ConfigStore).config.set({
      mode: 'demo',
      features: { ai: false, calendar: false },
    });
    await Promise.all([
      TestBed.inject(SettingsStore).refresh(),
      TestBed.inject(ProfileStore).refresh(),
      TestBed.inject(WalletStore).refresh(),
      TestBed.inject(WorkSessionStore).refresh(),
      TestBed.inject(SleepStore).refresh(),
      TestBed.inject(CalendarStore).refresh(),
    ]);
    fixture = TestBed.createComponent(SettingsPage);
    element = fixture.nativeElement as HTMLElement;
    await fixture.whenStable();
  }

  function button(text: string, root: ParentNode = element): HTMLButtonElement {
    const found = Array.from(root.querySelectorAll('button')).find(
      (item) => item.textContent.trim() === text,
    );
    if (!found) throw new Error(`button not found: ${text}`);
    return found;
  }

  function hasButton(text: string): boolean {
    return Array.from(element.querySelectorAll('button')).some(
      (item) => item.textContent.trim() === text,
    );
  }

  /** 開いている確認ダイアログ（overlay は document.body に付く）。 */
  function dialog(): HTMLElement {
    const found = document.body.querySelector<HTMLElement>('[role=dialog]');
    if (!found) throw new Error('dialog not found');
    return found;
  }

  function type(selector: string, value: string): void {
    const input = element.querySelector<HTMLInputElement | HTMLTextAreaElement>(selector);
    if (!input) throw new Error(`input not found: ${selector}`);
    input.value = value;
    input.dispatchEvent(new Event('input'));
  }

  async function settle(): Promise<void> {
    await flush();
    await fixture.whenStable();
  }

  beforeEach(() => {
    backend = new FakeBackend()
      .on('GET', '/settings', { body: settings() })
      .on('GET', '/profile', { body: profile })
      .on('GET', '/wallet', { body: wallet() })
      .on('GET', '/work-session', { body: { session: null } })
      .on('GET', '/sleep', { body: noSleep })
      .on('GET', '/shortcut', { body: { configured: false } })
      .on('GET', '/tasks', { body: taskList() })
      .on('GET', '/calendar', {
        body: {
          enabled: false,
          connected: false,
          sync: 'disconnected',
          failedJobs: 0,
          primaryEvents: false,
        },
      });
    TestBed.configureTestingModule({
      providers: [
        ...provideFakeBackend(backend),
        { provide: MATERIAL_ANIMATIONS, useValue: { animationsDisabled: true } },
      ],
    });
  });

  afterEach(() => {
    document.body.querySelector('.cdk-overlay-container')?.remove();
  });

  it('変更するまで保存できず、変えたまとまりだけを保存する', async () => {
    await render();
    expect(button('変更を保存').disabled).toBe(true);
    backend.on('PUT', '/settings', ({ body }) => ({
      body: { ...(body as object), revision: 2 },
    }));
    type('input[formcontrolname=snoozeMinutes]', '15');
    await settle();
    expect(element.textContent).toContain('変更内容はまだ保存されていません');
    button('変更を保存').click();
    await settle();
    expect(backend.count('PUT', '/settings')).toBe(1);
    expect(backend.count('PUT', '/profile')).toBe(0);
    expect(backend.requests.find((item) => item.method === 'PUT')?.body).toMatchObject({
      snoozeMinutes: 15,
    });
    expect(button('変更を保存').disabled).toBe(true);
  });

  it('範囲外のスヌーズでは保存できない', async () => {
    await render();
    type('input[formcontrolname=snoozeMinutes]', '121');
    await settle();
    expect(button('変更を保存').disabled).toBe(true);
  });

  it('作業の傾向の保存に失敗したら値を残し、再試行では失敗した方だけを送る', async () => {
    await render();
    backend
      .on('PUT', '/settings', ({ body }) => ({ body: { ...(body as object), revision: 2 } }))
      .on('PUT', '/profile', problem(503, 'external.failed'));
    type('input[formcontrolname=snoozeMinutes]', '20');
    type('input[formcontrolname=name]', '新しい名前');
    await settle();
    button('変更を保存').click();
    await settle();
    expect(element.querySelector('[role=alert]')?.textContent).toContain(
      '作業の傾向を保存できませんでした。',
    );
    expect(element.querySelector<HTMLInputElement>('input[formcontrolname=name]')?.value).toBe(
      '新しい名前',
    );

    backend.on('PUT', '/profile', ({ body }) => ({ body: { ...(body as object), revision: 2 } }));
    button('変更を保存').click();
    await settle();
    expect(backend.count('PUT', '/settings')).toBe(1);
    expect(backend.count('PUT', '/profile')).toBe(2);
  });

  it('言語を選んで保存すると、プロフィールに保存して画面の言語を切り替える', async () => {
    await render();
    backend.on('PUT', '/profile', ({ body }) => ({ body: { ...(body as object), revision: 2 } }));
    const select = await TestbedHarnessEnvironment.loader(fixture).getHarness(
      MatSelectHarness.with({ selector: '[formcontrolname=locale]' }),
    );
    await select.open();
    await select.clickOptions({ text: 'English' });
    await settle();
    button('変更を保存').click();
    await settle();
    expect(
      backend.requests.find((item) => item.method === 'PUT' && item.path === '/profile')?.body,
    ).toEqual({ name: '山田', context: '', timeZone: 'Asia/Tokyo', locale: 'en' });
    expect(TestBed.inject(LocaleStore).locale()).toBe('en');
    expect(hasButton('Save changes')).toBe(true);
  });

  it('視聴ルールを保存し、作業中は保存できない', async () => {
    await render();
    backend.on('PUT', '/wallet/policy', { body: wallet({ revision: 2 }) });
    button('視聴ルールを保存').click();
    await settle();
    const request = backend.requests.find((item) => item.path === '/wallet/policy');
    expect(request?.body).toEqual({ mode: 'rewards', dailyLimitSeconds: 1800 });
    expect(request?.headers.get('If-Match')).toBe('"1"');

    TestBed.inject(WorkSessionStore).accept(workSession());
    await settle();
    expect(button('視聴ルールを保存').disabled).toBe(true);
    expect(element.textContent).toContain('作業を記録してから設定を変更できます。');
  });

  it('視聴ルールの保存に失敗したら知らせる', async () => {
    await render();
    backend.on('PUT', '/wallet/policy', problem(409, 'work.active'));
    button('視聴ルールを保存').click();
    await settle();
    expect(element.textContent).toContain('保存できませんでした。');
  });

  it('就寝の予定を保存して外す', async () => {
    await render();
    const saved: SleepState = {
      revision: 2,
      sleep: {
        bedtime: '23:30',
        wake: '07:00',
        keptNights: 0,
        calendarPending: false,
        night: { start: '2026-11-08T13:30:00Z', end: '2026-11-08T22:00:00Z', active: false },
      },
    };
    backend.on('PUT', '/sleep', { body: saved });
    button('就寝の予定を保存').click();
    await settle();
    expect(
      backend.requests.find((item) => item.method === 'PUT' && item.path === '/sleep')?.body,
    ).toEqual({
      bedtime: '23:30',
      wake: '07:00',
    });
    expect(element.textContent).toContain('現在：23:30 就寝・07:00 起床');

    backend.on('DELETE', '/sleep', { body: { sleep: null, revision: 3 } });
    button('外す').click();
    await settle();
    expect(backend.count('DELETE', '/sleep')).toBe(1);
    expect(element.textContent).not.toContain('現在：23:30');
  });

  it('未連携なら「連携を解除」を出さない', async () => {
    await render();
    expect(hasButton('連携を解除')).toBe(false);
    expect(hasButton('Googleカレンダーを連携')).toBe(true);
  });

  it('連携済みなら確認してから解除し、連携状態と課題を取り直す', async () => {
    backend.on('GET', '/calendar', { body: connectedCalendar });
    await render();
    expect(hasButton('Googleカレンダーを連携')).toBe(false);
    button('連携を解除').click();
    await settle();
    expect(dialog().textContent).toContain('Googleカレンダーとの連携を解除しますか？');
    expect(dialog().textContent).toContain('登録した課題と予定はそのまま残ります。');
    expect(dialog().textContent).toContain('結び付きだけが外れます。');
    expect(dialog().textContent).toContain('いつでも連携し直せます。');
    expect(backend.count('POST', '/calendar/disconnect')).toBe(0);

    backend
      .on('POST', '/calendar/disconnect', { body: disconnectedCalendar })
      .on('GET', '/calendar', { body: disconnectedCalendar });
    button('連携を解除する', dialog()).click();
    await settle();
    expect(backend.count('POST', '/calendar/disconnect')).toBe(1);
    expect(
      backend.requests
        .find((item) => item.path === '/calendar/disconnect')
        ?.headers.get('Idempotency-Key'),
    ).toBeTruthy();
    expect(backend.count('GET', '/calendar')).toBe(2);
    expect(backend.count('GET', '/tasks')).toBe(1);
    expect(TestBed.inject(CalendarStore).connected()).toBe(false);
    expect(document.body.querySelector('[role=dialog]')).toBeNull();
    expect(document.body.textContent).toContain('Googleカレンダーとの連携を解除しました');
    expect(hasButton('連携を解除')).toBe(false);
    expect(hasButton('Googleカレンダーを連携')).toBe(true);
  });

  it('解除に失敗したら確認の中で知らせ、連携は残す', async () => {
    backend
      .on('GET', '/calendar', { body: connectedCalendar })
      .on('POST', '/calendar/disconnect', problem(503, 'external.failed'));
    await render();
    button('連携を解除').click();
    await settle();
    button('連携を解除する', dialog()).click();
    await settle();
    expect(dialog().querySelector('[role=alert]')?.textContent).toContain(
      messages.ja.shared.problems['external.failed'],
    );
    expect(backend.count('GET', '/calendar')).toBe(1);
    expect(backend.count('GET', '/tasks')).toBe(0);
    expect(TestBed.inject(CalendarStore).connected()).toBe(true);

    button('キャンセル', dialog()).click();
    await settle();
    expect(document.body.querySelector('[role=dialog]')).toBeNull();
    expect(document.body.textContent).not.toContain('Googleカレンダーとの連携を解除しました');
  });

  it('接続キーは発行したときだけ表示し、無効にできる', async () => {
    await render();
    backend
      .on('POST', '/shortcut/key', { status: 201, body: { key: 'abc.def' } })
      .on('GET', '/shortcut', { body: { configured: true, createdAt: '2026-11-08T01:00:00Z' } });
    button('接続キーを発行').click();
    await settle();
    expect(element.querySelector<HTMLInputElement>('input[readonly]')?.value).toBe('abc.def');
    expect(TestBed.inject(ShortcutStore).issuedKey()).toBe('abc.def');

    backend
      .on('DELETE', '/shortcut/key', { status: 204 })
      .on('GET', '/shortcut', { body: { configured: false } });
    button('無効にする').click();
    await settle();
    expect(element.querySelector('input[readonly]')).toBeNull();
    expect(button('接続キーを発行')).toBeTruthy();
  });
});
