import { SseParser } from '@dopagaki/contract/sse';

/** 管理画面が使う通知だけを公開する。再接続は RealtimeService が担当する。 */
export interface EventStream {
  onopen: (() => void) | null;
  onmessage: ((message: { data: unknown }) => void) | null;
  onerror: (() => void) | null;
  close(): void;
}

/** 標準の EventSource と違い、トークンを URL に出さずヘッダーで送る。 */
export class FetchEventSource implements EventStream {
  onopen: (() => void) | null = null;
  onmessage: ((message: { data: unknown }) => void) | null = null;
  onerror: (() => void) | null = null;
  private readonly controller = new AbortController();

  constructor(url: string, authorization?: string, request: typeof fetch = fetch) {
    void this.read(url, authorization, request);
  }

  close(): void {
    this.controller.abort();
  }

  private async read(url: string, authorization: string | undefined, request: typeof fetch) {
    const signal = this.controller.signal;
    // 中断は await やコールバックの間にも起きる。
    const stopped = () => signal.aborted;
    let reader: ReadableStreamDefaultReader<Uint8Array> | undefined;
    try {
      const response = await request(url, {
        headers: {
          Accept: 'text/event-stream',
          ...(authorization ? { Authorization: authorization } : {}),
        },
        credentials: 'same-origin',
        cache: 'no-store',
        signal,
      });
      if (stopped()) {
        await response.body?.cancel();
        return;
      }
      if (
        !response.ok ||
        !response.body ||
        response.headers.get('Content-Type')?.split(';')[0]?.trim() !== 'text/event-stream'
      ) {
        await response.body?.cancel();
        this.onerror?.();
        return;
      }
      reader = response.body.getReader();
      // fetch の中断に加え、テストや独自のストリームでも待機中の read を終わらせる。
      const cancel = () => {
        void reader?.cancel().catch(() => undefined);
      };
      signal.addEventListener('abort', cancel, { once: true });
      try {
        this.onopen?.();
        const parser = new SseParser();
        const decoder = new TextDecoder();
        while (!stopped()) {
          const { done, value } = await reader.read();
          if (stopped()) return;
          if (done) break;
          for (const event of parser.push(decoder.decode(value, { stream: true }))) {
            if (stopped()) return;
            if (event.event === 'message') this.onmessage?.({ data: event.data });
          }
        }
      } finally {
        signal.removeEventListener('abort', cancel);
        reader.releaseLock();
      }
      if (!stopped()) this.onerror?.();
    } catch {
      // エラー本文・URL・認証情報をログへ出さず、接続の再試行だけを依頼する。
      if (!stopped()) this.onerror?.();
    }
  }
}
