import { defaultDeadline, fromLocalInput } from './local-datetime';

describe('defaultDeadline', () => {
  it.each([
    [new Date(2030, 10, 8, 12, 7), '2030-11-08T12:15'],
    [new Date(2030, 10, 8, 12, 15), '2030-11-08T12:30'],
    [new Date(2030, 10, 8, 23, 59, 30), '2030-11-09T00:00'],
    [new Date(2030, 11, 31, 23, 59), '2031-01-01T00:00'],
  ])('現在時刻 %s より後の次の15分刻みは %s', (now, expected) => {
    const before = now.getTime();
    expect(defaultDeadline(now)).toBe(expected);
    expect(now.getTime()).toBe(before);
  });
});

describe('fromLocalInput', () => {
  it.each(['2030-11-08T24:00', '2030-11-08T25:61', '2030-11-08T12:7', '2030-02-30T12:17'])(
    '不正な日時 %s を受け付けない',
    (value) => {
      expect(fromLocalInput(value)).toBeUndefined();
    },
  );

  it('15分刻みではない時刻を保持する', () => {
    const iso = fromLocalInput('2030-11-08T12:17');
    expect(iso).toBe(new Date(2030, 10, 8, 12, 17).toISOString());
  });
});
