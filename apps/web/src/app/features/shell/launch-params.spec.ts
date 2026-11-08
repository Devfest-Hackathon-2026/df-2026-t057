import { parseLaunchQuery } from './launch-params';

describe('parseLaunchQuery', () => {
  it('通知・スヌーズ・使い方・カレンダー連携の結果を読む', () => {
    expect(parseLaunchQuery('?notification=t1&tutorial&calendar=connected')).toEqual({
      snooze: undefined,
      notification: 't1',
      tutorial: true,
      calendar: 'connected',
    });
    expect(parseLaunchQuery('?snooze=t2&calendar=other')).toEqual({
      snooze: 't2',
      notification: undefined,
      tutorial: false,
      calendar: undefined,
    });
  });
});
