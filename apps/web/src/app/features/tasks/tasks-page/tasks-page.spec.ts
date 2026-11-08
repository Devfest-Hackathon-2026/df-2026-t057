import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { FakeBackend, provideFakeBackend } from '../../../../testing/fake-backend';
import { plan, task, taskList } from '../../../../testing/fixtures';
import { TasksStore } from '../tasks.store';
import { TasksPage, filterTasks } from './tasks-page';

describe('filterTasks', () => {
  const items = [
    task({ id: 'late', deadline: '2026-11-12T00:00:00Z' }),
    task({ id: 'done', status: 'completed', deadline: '2026-11-09T00:00:00Z' }),
    task({ id: 'active', status: 'active', deadline: '2026-11-10T00:00:00Z' }),
    task({ id: 'early', deadline: '2026-11-09T00:00:00Z' }),
  ];

  it('未完了は作業中を含め、締切の早い順に並べる', () => {
    expect(filterTasks(items, 'pending').map((item) => item.id)).toEqual([
      'early',
      'active',
      'late',
    ]);
  });

  it('完了だけを出す', () => {
    expect(filterTasks(items, 'completed').map((item) => item.id)).toEqual(['done']);
  });
});

describe('TasksPage', () => {
  it('予定と見積もりが違えば両方を出す', async () => {
    const backend = new FakeBackend().on('GET', '/tasks', {
      body: taskList([
        task({ minutes: 30, plan: plan('2026-11-08T10:00:00Z', '2026-11-08T10:10:00Z') }),
      ]),
    });
    TestBed.configureTestingModule({
      providers: [...provideFakeBackend(backend), provideRouter([])],
    });
    await TestBed.inject(TasksStore).refresh();
    const fixture = TestBed.createComponent(TasksPage);
    await fixture.whenStable();
    const text = (fixture.nativeElement as HTMLElement).textContent;
    expect(text).toContain('予定 10分');
    expect(text).toContain('（見積もり 30分）');
  });

  it('課題がなければ空の表示を出す', async () => {
    const backend = new FakeBackend().on('GET', '/tasks', { body: taskList([]) });
    TestBed.configureTestingModule({
      providers: [...provideFakeBackend(backend), provideRouter([])],
    });
    await TestBed.inject(TasksStore).refresh();
    const fixture = TestBed.createComponent(TasksPage);
    await fixture.whenStable();
    expect((fixture.nativeElement as HTMLElement).textContent).toContain('まだ課題がありません');
  });
});
