import { displayedElapsedSeconds, workProgress } from './work-timer';

describe('displayedElapsedSeconds', () => {
  const asOf = 1_000_000;
  const cases: {
    name: string;
    session: { elapsedSeconds: number; limitSeconds: number; running: boolean };
    now: number;
    expected: number;
  }[] = [
    {
      name: '実行中は asOf からの経過を足す',
      session: { elapsedSeconds: 60, limitSeconds: 600, running: true },
      now: asOf + 5_900,
      expected: 65,
    },
    {
      name: '一時停止中は進めない',
      session: { elapsedSeconds: 60, limitSeconds: 600, running: false },
      now: asOf + 120_000,
      expected: 60,
    },
    {
      name: '上限で止める',
      session: { elapsedSeconds: 590, limitSeconds: 600, running: true },
      now: asOf + 60_000,
      expected: 600,
    },
    {
      name: '時計が戻っても減らさない',
      session: { elapsedSeconds: 30, limitSeconds: 600, running: true },
      now: asOf - 10_000,
      expected: 30,
    },
    {
      name: '上限ちょうど',
      session: { elapsedSeconds: 0, limitSeconds: 600, running: true },
      now: asOf + 600_000,
      expected: 600,
    },
    {
      name: '記録済みが上限を超えていても上限を表示する',
      session: { elapsedSeconds: 700, limitSeconds: 600, running: false },
      now: asOf,
      expected: 600,
    },
  ];
  for (const { name, session, now, expected } of cases) {
    it(name, () => {
      expect(displayedElapsedSeconds(session, asOf, now)).toBe(expected);
    });
  }
});

describe('workProgress', () => {
  it('割合を0〜100で返す', () => {
    expect(workProgress(150, 600)).toBe(25);
    expect(workProgress(900, 600)).toBe(100);
    expect(workProgress(0, 0)).toBe(100);
  });
});
