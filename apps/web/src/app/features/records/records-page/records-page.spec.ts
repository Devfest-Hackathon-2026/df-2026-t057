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
    TestBed.configureTestingModule({ providers: provideFakeBackend(backend) });
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
});
