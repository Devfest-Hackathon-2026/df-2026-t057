// テスト用の架空のデータ。
import type {
  Draft,
  Plan,
  Settings,
  Task,
  TaskList,
  Wallet,
  WorkSession,
} from '@dopagaki/contract';

export function plan(start = '2026-11-08T10:00:00Z', end = '2026-11-08T10:30:00Z'): Plan {
  return { start, end, steps: [{ title: 'レポートを書く', start, end }], reason: 'backward' };
}

export function task(overrides: Partial<Task> = {}): Task {
  return {
    id: 't1',
    title: 'レポート',
    notes: '',
    why: '',
    deadline: '2026-11-09T14:59:00Z',
    minutes: 30,
    url: '',
    status: 'pending',
    createdAt: '2026-11-08T00:00:00Z',
    plan: plan(),
    calendarLinked: false,
    ...overrides,
  };
}

export function taskList(
  items: Task[] = [task()],
  revision = 1,
  nextSnooze: Draft | null = null,
): TaskList {
  return { items, revision, nextSnooze };
}

export function draft(overrides: Partial<Draft> = {}): Draft {
  return {
    id: 'd1',
    kind: 'snooze',
    taskId: 't1',
    baseRevision: 1,
    createdAt: '2026-11-08T09:00:00Z',
    shortened: false,
    changes: [
      {
        taskId: 't1',
        title: 'レポート',
        before: plan(),
        after: plan('2026-11-08T10:10:00Z', '2026-11-08T10:40:00Z'),
        originalMinutes: 30,
        plannedSeconds: 1800,
      },
    ],
    ...overrides,
  };
}

export function settings(overrides: Partial<Settings> = {}): Settings {
  return {
    snoozeMinutes: 10,
    mode: 'pachinko',
    sound: true,
    reducedMotion: false,
    grime: false,
    revision: 1,
    ...overrides,
  };
}

export function wallet(overrides: Partial<Wallet> = {}): Wallet {
  return {
    revision: 1,
    balanceSeconds: 600,
    availableSeconds: 600,
    deferredSeconds: 0,
    policy: {
      mode: 'rewards',
      dailyLimitSeconds: 1800,
      day: '2026-11-08',
      resetAt: '2026-11-08T15:00:00Z',
      viewedSeconds: 0,
      rewardSeconds: 0,
      workSeconds: 0,
    },
    rules: {
      balanceCapSeconds: 1800,
      workSecondsPerReward: 300,
      rewardSecondsPerUnit: 60,
      dailyRewardCapSeconds: 1200,
      registrationRewardSeconds: 300,
      dailyRefillSeconds: 600,
      sleepBonusSeconds: 900,
      viewingChoicesSeconds: [300, 600],
    },
    ...overrides,
  };
}

export function workSession(overrides: Partial<WorkSession> = {}): WorkSession {
  return {
    id: 's1',
    taskId: 't1',
    startedAt: '2026-11-08T10:00:00Z',
    limitSeconds: 1800,
    elapsedSeconds: 0,
    running: true,
    asOf: '2026-11-08T10:00:00Z',
    ...overrides,
  };
}
