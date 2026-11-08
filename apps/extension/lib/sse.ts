// Server-Sent Events のテキストを、イベントに分ける（fetch で受け取るため EventSource を使わない）。
// service worker から Authorization ヘッダーを付けて購読したいので、パーサーを自前で持つ。
// https://html.spec.whatwg.org/multipage/server-sent-events.html#event-stream-interpretation

export interface ServerSentEvent {
  readonly event: string;
  readonly data: string;
  readonly id: string | undefined;
}

export class SseParser {
  #buffer = '';
  #data: string[] = [];
  #event = '';
  #id: string | undefined;

  /** 受け取った文字列を足し、そろったイベントを返す。 */
  push(chunk: string): ServerSentEvent[] {
    this.#buffer += chunk;
    const events: ServerSentEvent[] = [];
    for (;;) {
      const match = /\r\n|\r|\n/.exec(this.#buffer);
      if (!match) break;
      // 末尾の \r だけでは \r\n の途中かもしれないので、次の文字を待つ。
      if (match[0] === '\r' && match.index === this.#buffer.length - 1) break;
      const line = this.#buffer.slice(0, match.index);
      this.#buffer = this.#buffer.slice(match.index + match[0].length);
      const event = this.#line(line);
      if (event) events.push(event);
    }
    return events;
  }

  #line(line: string): ServerSentEvent | undefined {
    if (line === '') {
      if (this.#data.length === 0) {
        this.#event = '';
        return undefined;
      }
      const event = { event: this.#event || 'message', data: this.#data.join('\n'), id: this.#id };
      this.#data = [];
      this.#event = '';
      return event;
    }
    if (line.startsWith(':')) return undefined;
    const colon = line.indexOf(':');
    const field = colon === -1 ? line : line.slice(0, colon);
    let value = colon === -1 ? '' : line.slice(colon + 1);
    if (value.startsWith(' ')) value = value.slice(1);
    if (field === 'data') this.#data.push(value);
    else if (field === 'event') this.#event = value;
    else if (field === 'id' && !value.includes('\0')) this.#id = value;
    return undefined;
  }
}

/** 再接続の待ち時間（1秒から倍々に、最大60秒）。 */
export function reconnectDelayMs(attempt: number): number {
  return Math.min(60_000, 1000 * 2 ** Math.max(0, attempt));
}
