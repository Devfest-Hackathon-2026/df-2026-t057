import { describe, expect, it } from 'vitest';
import {
  MAX_REPORT_SECONDS,
  ViewingMeter,
  chargedSeconds,
  retryDelayMs,
  type Receipt,
} from './meter';

function play(meter: ViewingMeter, seconds: number) {
  for (let i = 0; i < seconds; i += 1) meter.tick(true, 1);
}

describe('ViewingMeter.tick', () => {
  it('counts only consecutive playing ticks', () => {
    const meter = new ViewingMeter();
    meter.tick(true, 1); // 再生を確認しただけ
    expect(meter.unreportedSeconds).toBe(0);
    meter.tick(true, 1);
    meter.tick(true, 1);
    expect(meter.unreportedSeconds).toBe(2);
    meter.tick(false, 1);
    meter.tick(true, 1);
    expect(meter.unreportedSeconds).toBe(2);
  });

  it('caps a delayed tick at two seconds', () => {
    const meter = new ViewingMeter();
    meter.tick(true, 1);
    meter.tick(true, 60);
    expect(meter.unreportedSeconds).toBe(2);
    meter.tick(true, -5);
    expect(meter.unreportedSeconds).toBe(2);
  });

  it('starts over after an interruption', () => {
    const meter = new ViewingMeter();
    play(meter, 3);
    meter.interrupt();
    meter.tick(true, 1);
    expect(meter.unreportedSeconds).toBe(2);
  });
});

describe('ViewingMeter reports', () => {
  it('reports about every 30 seconds while playing', () => {
    const meter = new ViewingMeter();
    play(meter, 30);
    expect(meter.shouldReport(0, true)).toBe(false); // 29 秒
    meter.tick(true, 1);
    expect(meter.shouldReport(0, true)).toBe(true);
  });

  it('reports what is left as soon as playback stops', () => {
    const meter = new ViewingMeter();
    play(meter, 3);
    expect(meter.shouldReport(0, true)).toBe(false);
    expect(meter.shouldReport(0, false)).toBe(true);
    expect(meter.shouldReport(0, true, true)).toBe(true);
  });

  it('never sends more than the API accepts', () => {
    const meter = new ViewingMeter();
    play(meter, 200);
    const receipt = meter.begin(() => 'receipt-1', 's1', 0);
    expect(receipt.seconds).toBe(MAX_REPORT_SECONDS);
  });

  it('reuses the same receipt key when retrying', () => {
    const meter = new ViewingMeter();
    play(meter, 31);
    let next = 0;
    const id = () => `receipt-${++next}`;
    const first = meter.begin(id, 's1', 10);
    expect(meter.shouldReport(0, true)).toBe(false); // 送信中
    meter.fail(1000);
    expect(meter.shouldReport(5000, true)).toBe(false); // 待ち時間中
    expect(meter.shouldReport(11_000, true)).toBe(true);
    play(meter, 10);
    const retry = meter.begin(id, 's1', 10);
    expect(retry).toBe(first);
    expect(retry.id).toBe('receipt-1');
    meter.succeed(30);
    expect(meter.unreportedSeconds).toBe(10);
    expect(meter.begin(id, 's1', 40).id).toBe('receipt-2');
  });

  it('keeps a partially charged remainder but drops an uncharged report', () => {
    const meter = new ViewingMeter();
    play(meter, 31);
    meter.begin(() => 'r1', 's1', 0);
    meter.succeed(20);
    expect(meter.unreportedSeconds).toBe(10);
    meter.begin(() => 'r2', 's1', 20);
    meter.succeed(0);
    expect(meter.unreportedSeconds).toBe(0);
  });

  it('discards a rejected report', () => {
    const meter = new ViewingMeter();
    play(meter, 31);
    meter.begin(() => 'r1', 's1', 0);
    meter.discard();
    expect(meter.receipt).toBeUndefined();
    expect(meter.unreportedSeconds).toBe(0);
  });

  it('computes the remaining time from unreported seconds', () => {
    const meter = new ViewingMeter();
    play(meter, 11);
    expect(meter.remaining(15)).toBe(5);
  });
});

describe('chargedSeconds', () => {
  const receipt: Receipt = { id: 'r', seconds: 30, sessionId: 's1', consumedBefore: 100 };
  it.each([
    [{ sessionId: 's1', consumedSeconds: 130 }, true, 30],
    [{ sessionId: 's1', consumedSeconds: 110 }, true, 10],
    [{ sessionId: 's1', consumedSeconds: 90 }, true, 0],
    [{ sessionId: 's1', consumedSeconds: 500 }, true, 30],
    [{ sessionId: 's2', consumedSeconds: 5 }, true, 30],
    [{ sessionId: undefined, consumedSeconds: undefined }, true, 30],
    [{ sessionId: 's1', consumedSeconds: 110 }, false, 30],
  ])('%o (session required: %s) → %i', (result, requiresSession, expected) => {
    expect(chargedSeconds(receipt, result, requiresSession)).toBe(expected);
  });
});

describe('retryDelayMs', () => {
  it.each([
    [1, 10_000],
    [2, 20_000],
    [3, 40_000],
    [4, 60_000],
    [10, 60_000],
  ])('failure %i waits %i ms', (failures, expected) => {
    expect(retryDelayMs(failures)).toBe(expected);
  });
});
