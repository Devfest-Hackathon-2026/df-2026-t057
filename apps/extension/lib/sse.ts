export { SseParser } from '@dopagaki/contract/sse';

/** 再接続の待ち時間（1秒から倍々に、最大60秒）。 */
export function reconnectDelayMs(attempt: number): number {
  return Math.min(60_000, 1000 * 2 ** Math.max(0, attempt));
}
