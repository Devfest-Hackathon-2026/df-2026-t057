import { provideRouter } from '@angular/router';
import { TestBed } from '@angular/core/testing';
import type { LedgerEntry, Records } from '@dopagaki/contract';
import { FakeBackend, flush, provideFakeBackend } from '../../../../testing/fake-backend';
import { wallet } from '../../../../testing/fixtures';
import { RecordsPage, layoutDays, weekday } from './records-page';

function days(): Records['days'] {
  const start = Date.UTC(2026, 7, 17);
  return Array.from({ length: 84 }, (_, index) => {
    const date = new Date(start + index * 86_400_000).toISOString().slice(0, 10);
    return { date, completed: index === 83 ? 2 : 0, earnedSeconds: 0 };
  });
}

const records: Records = {
  days: days(),
  totals: { completed: 2, earnedSeconds: 0 },
  practice: {
    viewingSessions: 3,
    endedSessions: 2,
    extensions: 1,
    startedTasks: 2,
    onTimeStarts: 1,
    missedStarts: 0,
    history: [],
  },
};

const entries: LedgerEntry[] = [
  {
    id: 'l2',
    kind: 'work',
    seconds: 75,
    at: '2026-11-08T03:00:00Z',
    deferred: true,
    taskId: 't1',
    taskTitle: 'レポート',
    workSeconds: 375,
  },
  { id: 'l1', kind: 'sleep', seconds: 300, at: '2026-11-08T01:00:00Z', deferred: false },
  { id: 'l0', kind: 'viewing', seconds: -120, at: '2026-11-07T01:00:00Z', deferred: false },
];

describe('weekday / layoutDays', () => {
  it('日付の文字列から曜日を決め、日曜始まりで空けて並べる', () => {
    expect(weekday('2026-11-08')).toBe(0);
    expect(weekday('2026-08-17')).toBe(1);
    const cells = layoutDays(records.days);
    expect(cells[0]).toBeNull();
    expect(cells[1]?.date).toBe('2026-08-17');
    expect(cells).toHaveLength(85);
  });
});

describe('RecordsPage', () => {
  it('APIの集計で84日のマスと台帳を表示する', async () => {
    const backend = new FakeBackend()
      .on('GET', '/records', { body: records })
      .on('GET', '/wallet/ledger', { body: { entries } })
      .on('GET', '/wallet', { body: wallet() });
    TestBed.configureTestingModule({
      providers: [provideRouter([]), ...provideFakeBackend(backend)],
    });
    const fixture = TestBed.createComponent(RecordsPage);
    await flush();
    await fixture.whenStable();
    const element = fixture.nativeElement as HTMLElement;

    const cells = element.querySelectorAll('button.day');
    expect(cells).toHaveLength(84);
    expect(element.querySelectorAll('button.day.multiple')).toHaveLength(1);
    expect(element.querySelector('.count')?.textContent).toContain('2');
    expect(element.querySelector('.selected')?.textContent).toContain(
      '11月8日 · 2件完了 · 視聴時間 ＋0秒',
    );
    expect(cells[83]?.getAttribute('aria-label')).toBe('2026/11/8: 2件完了、視聴時間 ＋0秒');

    const rows = Array.from(element.querySelectorAll('.ledger-row'), (row) => row.textContent);
    expect(rows[0]).toContain('レポート');
    expect(rows[0]).toContain('朝に受け取り');
    expect(rows[0]).toContain('作業 6分15秒');
    expect(rows[0]).toContain('＋1分15秒');
    expect(rows[1]).toContain('睡眠の報酬');
    expect(rows[1]).toContain('＋5分');
    expect(rows[2]).toContain('−2分');
  });
  it('今日の時間と開始の母数を表示し、詳細は折りたたむ', async () => {
    const current = wallet();
    current.policy.workSeconds = 1500;
    current.policy.viewedSeconds = 600;
    const backend = new FakeBackend()
      .on('GET', '/records', {
        body: { ...records, practice: { ...records.practice, missedStarts: 1 } },
      })
      .on('GET', '/wallet/ledger', { body: { entries: [] } })
      .on('GET', '/wallet', { body: current });
    TestBed.configureTestingModule({
      providers: [provideRouter([]), ...provideFakeBackend(backend)],
    });
    const fixture = TestBed.createComponent(RecordsPage);
    await flush();
    await fixture.whenStable();
    const element = fixture.nativeElement as HTMLElement;
    const review = element.querySelector('.reflection');
    expect(review?.textContent).toContain('視聴と作業の振り返り');
    expect(review?.querySelector('.metrics')?.textContent).toContain('25分');
    expect(review?.querySelector('.metrics')?.textContent).toContain('10分');
    expect(review?.textContent).toContain('1／2件');
    expect(review?.querySelector('a')?.getAttribute('href')).toBe('/tasks');
    expect(review?.querySelector('details')?.open).toBe(false);
  });

  it('未記録を成功やゼロの週として並べず、記録した週は新しい順に表示する', async () => {
    const data: Records = {
      ...records,
      practice: {
        ...records.practice,
        startedTasks: 0,
        onTimeStarts: 0,
        missedStarts: 0,
        history: [{ date: '2026-11-08', viewedSeconds: 0, workSeconds: 0 }],
      },
      weeks: [
        { start: '2026-10-19', starts: 0, onTime: 0, snoozes: 0, workSeconds: 0 },
        { start: '2026-10-26', starts: 2, onTime: 1, snoozes: 0, workSeconds: 1500 },
        { start: '2026-11-02', starts: 0, onTime: 0, snoozes: 1, workSeconds: 0 },
      ],
    };
    const backend = new FakeBackend()
      .on('GET', '/records', { body: data })
      .on('GET', '/wallet/ledger', { body: { entries: [] } })
      .on('GET', '/wallet', { body: wallet() });
    TestBed.configureTestingModule({
      providers: [provideRouter([]), ...provideFakeBackend(backend)],
    });
    const fixture = TestBed.createComponent(RecordsPage);
    await flush();
    await fixture.whenStable();
    const element = fixture.nativeElement as HTMLElement;
    expect(element.querySelector('.reflection')?.textContent).toContain(
      '作業や視聴の記録ができると',
    );
    expect(element.querySelector('.reflection')?.textContent).toContain(
      '過去7日間に開始した課題はありません',
    );
    expect(element.querySelector('.reflection a')).toBeNull();
    const weeks = element.querySelectorAll('.week');
    expect(weeks).toHaveLength(2);
    expect(weeks[0]?.textContent).toContain('11月2日からの週');
    expect(weeks[0]?.textContent).toContain('開始の記録なし');
    expect(weeks[1]?.textContent).toContain('1／2回');
    expect(weeks[1]?.textContent).toContain('25分');
  });

  it('週の記録がない場合は案内を表示する', async () => {
    const backend = new FakeBackend()
      .on('GET', '/records', { body: { ...records, weeks: [] } })
      .on('GET', '/wallet/ledger', { body: { entries: [] } })
      .on('GET', '/wallet', { body: wallet() });
    TestBed.configureTestingModule({
      providers: [provideRouter([]), ...provideFakeBackend(backend)],
    });
    const fixture = TestBed.createComponent(RecordsPage);
    await flush();
    await fixture.whenStable();
    expect((fixture.nativeElement as HTMLElement).querySelector('.weekly')?.textContent).toContain(
      '記録のない週は評価しません',
    );
  });
});
