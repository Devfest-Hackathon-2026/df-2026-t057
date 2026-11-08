// 変更通知（GET /api/events）の購読。データ本体は来ないので、通知されたリソースだけを取り直す。
// service worker は止められるので、content script がつながっている間だけ購読する。
// それ以外の間は chrome.alarms で定期的に取り直す（background.ts）。
import { reconnectDelayMs, SseParser } from '../lib/sse';
import { resourcesToRefetch } from '../lib/resources';
import { changeEventSchema } from '../lib/schemas';
import { openEventStream } from './api';
import { getConnection } from './connection';
import { knownRevisions, rememberRevision } from './store';
import { refetch } from './sync';

let controller: AbortController | undefined;
let healthy = false;

/** 購読中で、最後の接続がうまくいっているか（定期の取り直しを省くのに使う）。 */
export function streamHealthy(): boolean {
  return controller !== undefined && healthy;
}

export function startStream(): void {
  if (controller) return;
  const current = new AbortController();
  controller = current;
  void run(current.signal);
}

export function stopStream(): void {
  controller?.abort();
  controller = undefined;
  healthy = false;
}

/** 接続（トークンやアカウント）が変わったら、新しい Authorization でつなぎ直す。 */
export function restartStream(): void {
  if (!controller) return;
  stopStream();
  startStream();
}

function wait(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    const timer = setTimeout(resolve, ms);
    signal.addEventListener('abort', () => {
      clearTimeout(timer);
      resolve();
    });
  });
}

async function run(signal: AbortSignal): Promise<void> {
  // signal.aborted は await の間に変わるので、毎回読み直す。
  const stopped = () => signal.aborted;
  let attempt = 0;
  while (!stopped()) {
    try {
      if (!(await getConnection())) {
        healthy = false;
        await wait(reconnectDelayMs(attempt++), signal);
        continue;
      }
      const response = await openEventStream(signal);
      if (!response.ok || !response.body) throw new Error(`events: ${response.status}`);
      healthy = true;
      attempt = 0;
      await consume(response.body, signal);
    } catch (error) {
      if (stopped()) return;
      console.warn('DOPAGAKI: change notifications interrupted', error);
    }
    healthy = false;
    if (!stopped()) await wait(reconnectDelayMs(attempt++), signal);
  }
}

async function consume(body: ReadableStream<Uint8Array>, signal: AbortSignal): Promise<void> {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  const parser = new SseParser();
  signal.addEventListener('abort', () => {
    void reader.cancel().catch(() => undefined);
  });
  for (;;) {
    const { done, value } = await reader.read();
    if (done || signal.aborted) return;
    for (const event of parser.push(decoder.decode(value, { stream: true }))) {
      let data: unknown;
      try {
        data = JSON.parse(event.data);
      } catch {
        continue;
      }
      const parsed = changeEventSchema.safeParse(data);
      if (!parsed.success) continue;
      const change = parsed.data;
      const stale = resourcesToRefetch(change, knownRevisions());
      if (stale.length === 0) continue;
      // 取り直せたときだけ版を覚える（失敗したら、次の通知や定期の取り直しでもう一度取る）。
      void refetch(stale)
        .then(() => {
          rememberRevision(change.resource, change.revision);
        })
        .catch((error: unknown) => {
          console.warn('DOPAGAKI: refetch failed', error);
        });
    }
  }
}
