import { describe, expect, it } from 'vitest';
import { SseParser, reconnectDelayMs } from './sse';

describe('SseParser', () => {
  it('parses events split across chunks', () => {
    const parser = new SseParser();
    expect(parser.push('data: {"resource":"wal')).toEqual([]);
    expect(parser.push('let","revision":3}\n')).toEqual([]);
    expect(parser.push('\n')).toEqual([
      { event: 'message', data: '{"resource":"wallet","revision":3}', id: undefined },
    ]);
  });

  it('ignores comments (keep-alive) and joins multi-line data', () => {
    const parser = new SseParser();
    expect(parser.push(': ping\n\nevent: change\nid: 7\ndata: a\ndata:b\n\n')).toEqual([
      { event: 'change', data: 'a\nb', id: '7' },
    ]);
  });

  it('accepts CRLF and CR line endings, including a CRLF split between chunks', () => {
    const parser = new SseParser();
    expect(parser.push('data: x\r')).toEqual([]);
    expect(parser.push('\n\r\n')).toEqual([{ event: 'message', data: 'x', id: undefined }]);
    expect(parser.push('data: y\r\rdata: z')).toEqual([
      { event: 'message', data: 'y', id: undefined },
    ]);
  });

  it('does not dispatch an event without data', () => {
    const parser = new SseParser();
    expect(parser.push('event: change\n\n')).toEqual([]);
    expect(parser.push('data: z\n\n')).toEqual([{ event: 'message', data: 'z', id: undefined }]);
  });
});

describe('reconnectDelayMs', () => {
  it.each([
    [0, 1000],
    [1, 2000],
    [5, 32_000],
    [6, 60_000],
    [20, 60_000],
  ])('attempt %i → %i ms', (attempt, expected) => {
    expect(reconnectDelayMs(attempt)).toBe(expected);
  });
});
