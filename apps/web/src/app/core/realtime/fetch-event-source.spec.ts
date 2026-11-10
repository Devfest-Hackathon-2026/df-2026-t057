import { FetchEventSource } from './fetch-event-source';

async function settle(): Promise<void> {
  for (let i = 0; i < 10; i++) await Promise.resolve();
}

function stream() {
  let writer!: ReadableStreamDefaultController<Uint8Array>;
  const cancel = vi.fn();
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      writer = controller;
    },
    cancel,
  });
  const request = vi.fn<typeof fetch>().mockResolvedValue(
    new Response(body, {
      headers: { 'Content-Type': 'text/event-stream; charset=utf-8' },
    }),
  );
  const source = new FetchEventSource('/api/events', 'Bearer fixture-token', request);
  const onopen = vi.fn();
  const onmessage = vi.fn();
  const onerror = vi.fn();
  Object.assign(source, { onopen, onmessage, onerror });
  return { source, writer, request, cancel, onopen, onmessage, onerror };
}

describe('FetchEventSource', () => {
  it('トークンはヘッダーだけに付け、Cookie を維持し、分割された日本語の通知を読む', async () => {
    const s = stream();
    await settle();
    expect(s.request).toHaveBeenCalledWith('/api/events', {
      headers: { Accept: 'text/event-stream', Authorization: 'Bearer fixture-token' },
      credentials: 'same-origin',
      cache: 'no-store',
      signal: s.request.mock.calls[0]?.[1]?.signal,
    });
    expect(s.onopen).toHaveBeenCalledOnce();
    const bytes = new TextEncoder().encode(
      ': ping\r\n\r\ndata: {"resource":"tasks","revision":2,"memo":"課題"}\r\n\r\n',
    );
    for (const byte of bytes) s.writer.enqueue(new Uint8Array([byte]));
    await settle();
    // 1バイトずつのチャンクをすべて読み終える。
    await vi.waitFor(() => {
      expect(s.onmessage).toHaveBeenCalledOnce();
    });
    expect(s.onmessage).toHaveBeenCalledWith({
      data: '{"resource":"tasks","revision":2,"memo":"課題"}',
    });
    s.source.close();
    await settle();
    expect(s.cancel).toHaveBeenCalledOnce();
    expect(s.onerror).not.toHaveBeenCalled();
  });

  it('デモは Authorization を付けず同一オリジンの Cookie で接続する', async () => {
    const request = vi.fn<typeof fetch>().mockResolvedValue(new Response(null, { status: 401 }));
    new FetchEventSource('/api/events', undefined, request);
    await settle();
    expect(request.mock.calls[0]?.[1]?.headers).toEqual({ Accept: 'text/event-stream' });
    expect(request.mock.calls[0]?.[1]?.credentials).toBe('same-origin');
  });

  it.each([401, 403, 429, 500])('HTTP %i は接続成功にせず再接続を依頼する', async (status) => {
    const request = vi.fn<typeof fetch>().mockResolvedValue(new Response('{}', { status }));
    const source = new FetchEventSource('/api/events', 'Bearer fixture-token', request);
    const onopen = vi.fn();
    const onerror = vi.fn();
    Object.assign(source, { onopen, onerror });
    await settle();
    expect(onopen).not.toHaveBeenCalled();
    expect(onerror).toHaveBeenCalledOnce();
  });

  it('HTMLへのリダイレクト応答を通知として扱わない', async () => {
    const request = vi.fn<typeof fetch>().mockResolvedValue(
      new Response('<html>', {
        headers: { 'Content-Type': 'text/html' },
      }),
    );
    const source = new FetchEventSource('/api/events', undefined, request);
    const onerror = vi.fn();
    source.onerror = onerror;
    await settle();
    expect(onerror).toHaveBeenCalledOnce();
  });

  it('EOF とネットワーク切断の両方で再接続を依頼する', async () => {
    for (const fail of [false, true]) {
      const s = stream();
      await settle();
      if (fail) s.writer.error(new TypeError('offline'));
      else s.writer.close();
      await settle();
      expect(s.onerror).toHaveBeenCalledOnce();
      s.source.close();
    }
  });

  it('応答待ちで閉じても、遅い応答や通知を採用せず再接続しない', async () => {
    let resolve!: (response: Response) => void;
    const request = vi.fn<typeof fetch>().mockImplementation(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    const source = new FetchEventSource('/api/events', 'Bearer old-token', request);
    const onopen = vi.fn();
    const onmessage = vi.fn();
    const onerror = vi.fn();
    Object.assign(source, { onopen, onmessage, onerror });
    source.close();
    expect(request.mock.calls[0]?.[1]?.signal?.aborted).toBe(true);
    const cancel = vi.fn();
    resolve(
      new Response(new ReadableStream({ cancel }), {
        headers: { 'Content-Type': 'text/event-stream' },
      }),
    );
    await settle();
    expect(cancel).toHaveBeenCalledOnce();
    expect(onopen).not.toHaveBeenCalled();
    expect(onmessage).not.toHaveBeenCalled();
    expect(onerror).not.toHaveBeenCalled();
  });
});
