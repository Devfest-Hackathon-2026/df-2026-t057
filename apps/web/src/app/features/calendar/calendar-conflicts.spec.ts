import { TestBed } from '@angular/core/testing';
import { FakeBackend, flush, provideFakeBackend } from '../../../testing/fake-backend';
import { CalendarConflicts } from './calendar-conflicts';

describe('CalendarConflicts', () => {
  it('同期の再試行に失敗したら、そのエラーを消さずに出す', async () => {
    const backend = new FakeBackend()
      .on('GET', '/calendar/conflicts', { body: [] })
      .on('POST', '/calendar/retry', {
        status: 503,
        body: { type: 'about:blank', status: 503, code: 'calendar.unavailable' },
      })
      .on('GET', '/calendar', {
        body: {
          enabled: true,
          connected: true,
          sync: 'pending',
          failedJobs: 1,
          primaryEvents: false,
        },
      });
    TestBed.configureTestingModule({ providers: provideFakeBackend(backend) });
    const fixture = TestBed.createComponent(CalendarConflicts);
    const conflicts = fixture.componentInstance;
    await conflicts.retry();
    await flush();
    await fixture.whenStable();
    expect(backend.count('GET', '/calendar/conflicts')).toBe(0);
    expect((fixture.nativeElement as HTMLElement).querySelector('[role="alert"]')).not.toBeNull();
  });
});
