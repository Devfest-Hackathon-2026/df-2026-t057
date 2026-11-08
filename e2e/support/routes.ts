import type { Page, Route } from '@playwright/test';
import type { AssistantCard, AssistantTurn, Config, ProblemCode } from './api';

/** RFC 9457 の Problem Details で失敗させる（画面は `code` から文言を決める）。 */
export function fulfillProblem(
  route: Route,
  status: number,
  code: ProblemCode,
  headers: Record<string, string> = {},
): Promise<void> {
  return route.fulfill({
    status,
    headers,
    contentType: 'application/problem+json',
    body: JSON.stringify({ type: 'about:blank', status, code }),
  });
}

/** 押すまで応答を止めておく（処理中の表示を確かめる）。 */
export class Gate {
  readonly #opened: Promise<void>;
  #open!: () => void;

  constructor() {
    this.#opened = new Promise<void>((resolve) => {
      this.#open = resolve;
    });
  }

  open(): void {
    this.#open();
  }

  wait(): Promise<void> {
    return this.#opened;
  }
}

/** `GET /api/config` の一部を差し替える。 */
export async function patchConfig(page: Page, patch: (config: Config) => Config): Promise<void> {
  await page.route('**/api/config', async (route) => {
    const response = await route.fetch();
    const config = (await response.json()) as Config;
    await route.fulfill({ response, json: patch(config) });
  });
}

/** AI を使える環境として表示させる（デモのサーバーは AI を持たないので、相談の応答はテストで返す）。 */
export function enableAi(page: Page): Promise<void> {
  return patchConfig(page, (config) => ({ ...config, features: { ...config.features, ai: true } }));
}

export interface AssistantRequest {
  readonly id: string;
  readonly text: string;
  readonly taskId: string | undefined;
}

export interface AssistantMock {
  /** これまでに返した相談（`GET /api/assistant/turns` が返す）。 */
  readonly history: AssistantTurn[];
  readonly requests: AssistantRequest[];
}

/**
 * 相談の API をテストの応答に差し替える。`reply` は `POST /api/assistant/turns` の本文と
 * `Idempotency-Key`（相談の ID）を受け取り、返答とカードを返す。
 */
export async function mockAssistant(
  page: Page,
  reply: (request: AssistantRequest) => Promise<{ reply: string; cards: AssistantCard[] }>,
): Promise<AssistantMock> {
  const mock: AssistantMock = { history: [], requests: [] };
  await page.route('**/api/assistant/turns', async (route) => {
    const request = route.request();
    if (request.method() === 'GET') {
      await route.fulfill({ json: { turns: mock.history } });
      return;
    }
    const body = request.postDataJSON() as { text: string; taskId?: string };
    const input: AssistantRequest = {
      id: (await request.headerValue('idempotency-key')) ?? 'missing-key',
      text: body.text,
      taskId: body.taskId,
    };
    mock.requests.push(input);
    const answer = await reply(input);
    const turn: AssistantTurn = {
      id: input.id,
      text: input.text,
      ...(input.taskId === undefined ? {} : { taskId: input.taskId }),
      status: 'ready',
      createdAt: new Date().toISOString(),
      ...answer,
    };
    mock.history.splice(0, mock.history.length, turn);
    await route.fulfill({ json: turn });
  });
  return mock;
}
