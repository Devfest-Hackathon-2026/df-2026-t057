import { randomBytes } from 'node:crypto';
import { expect, type APIRequestContext, type APIResponse } from '@playwright/test';
import type { components } from '../../packages/contract/src/generated/schema';

type Schemas = components['schemas'];
export type Task = Schemas['Task'];
export type TaskInput = Schemas['TaskInput'];
export type TaskCreateInput = Schemas['TaskCreateInput'];
export type TaskList = Schemas['TaskList'];
export type Plan = Schemas['Plan'];
export type Draft = Schemas['Draft'];
export type Wallet = Schemas['Wallet'];
export type LedgerEntry = Schemas['LedgerEntry'];
export type Profile = Schemas['Profile'];
export type Settings = Schemas['Settings'];
export type SleepState = Schemas['SleepState'];
export type WorkSession = Schemas['WorkSession'];
export type WorkRecord = Schemas['WorkRecord'];
export type Viewing = Schemas['Viewing'];
export type Records = Schemas['Records'];
export type Config = Schemas['Config'];
export type CalendarStatus = Schemas['CalendarStatus'];
export type AssistantTurn = Schemas['AssistantTurn'];
export type AssistantCard = Schemas['AssistantCard'];
export type ProblemCode = Schemas['ProblemCode'];

/** 状態を変えるリクエストの `Idempotency-Key`（`^[A-Za-z0-9_-]{8,80}$`）。 */
export function idempotencyKey(): string {
  return randomBytes(16).toString('hex');
}

/** 今から `hours` 時間後（ISO 8601）。 */
export function hoursFromNow(hours: number): string {
  return new Date(Date.now() + hours * 3_600_000).toISOString();
}

/** 課題の入力。指定しなかった項目は空にする。 */
export function taskInput(input: Partial<TaskCreateInput> & { title: string }): TaskCreateInput {
  return {
    notes: '',
    why: '',
    url: '',
    minutes: 30,
    deadline: hoursFromNow(4),
    ...input,
  };
}

/** 登録用の入力から ID を外す（プレビューと変更の入力は ID を持たない）。 */
export function withoutId(input: TaskCreateInput): TaskInput {
  return {
    title: input.title,
    notes: input.notes,
    why: input.why,
    deadline: input.deadline,
    minutes: input.minutes,
    url: input.url,
  };
}

async function json<T>(response: APIResponse, what: string): Promise<T> {
  expect(response.ok(), `${what}: ${String(response.status())} ${await response.text()}`).toBe(
    true,
  );
  return (await response.json()) as T;
}

/**
 * テストの準備と確認に使う API の呼び出し。管理画面と同じデモの Cookie で、同じ利用者として呼ぶ。
 * 画面の操作の代わりに状態を作るときと、画面の操作の結果を確かめるときに使う。
 */
export class Api {
  constructor(private readonly request: APIRequestContext) {}

  /** デモの Cookie を発行させる（以後の画面と API が同じ利用者になる）。 */
  async signIn(): Promise<void> {
    await this.profile();
  }

  private async get<T>(path: string): Promise<T> {
    return json<T>(await this.request.get(`/api${path}`), `GET ${path}`);
  }

  private async send<T>(
    method: 'post' | 'put' | 'delete',
    path: string,
    options: { data?: unknown; revision?: number } = {},
  ): Promise<T> {
    const headers: Record<string, string> = { 'Idempotency-Key': idempotencyKey() };
    if (options.revision !== undefined) headers['If-Match'] = `"${String(options.revision)}"`;
    const response = await this.request[method](`/api${path}`, {
      headers,
      ...(options.data === undefined ? {} : { data: options.data }),
    });
    if (response.status() === 204) {
      expect(response.ok()).toBe(true);
      return undefined as T;
    }
    return json<T>(response, `${method.toUpperCase()} ${path}`);
  }

  profile(): Promise<Profile> {
    return this.get('/profile');
  }

  settings(): Promise<Settings> {
    return this.get('/settings');
  }

  tasks(): Promise<TaskList> {
    return this.get('/tasks');
  }

  async task(id: string): Promise<Task> {
    return this.get(`/tasks/${id}`);
  }

  createTask(input: Partial<TaskCreateInput> & { title: string }): Promise<Task> {
    return this.send('post', '/tasks', { data: taskInput(input) });
  }

  previewNewTask(input: TaskInput): Promise<{ plan: Plan; revision: number }> {
    return this.send('post', '/tasks/preview', { data: input });
  }

  previewTaskEdit(id: string, input: TaskInput): Promise<Draft> {
    return this.send('post', `/tasks/${id}/edit-preview`, { data: input });
  }

  startTask(id: string): Promise<WorkSession> {
    return this.send('post', `/tasks/${id}/start`);
  }

  completeTask(id: string): Promise<{ task: Task; work?: WorkRecord }> {
    return this.send('post', `/tasks/${id}/complete`);
  }

  async workSession(): Promise<WorkSession | null> {
    return (await this.get<{ session: WorkSession | null }>('/work-session')).session;
  }

  confirmWork(sessionId: string): Promise<WorkRecord> {
    return this.send('post', '/work-session/confirm', { data: { sessionId } });
  }

  wallet(): Promise<Wallet> {
    return this.get('/wallet');
  }

  async ledger(): Promise<LedgerEntry[]> {
    return (await this.get<{ entries: LedgerEntry[] }>('/wallet/ledger')).entries;
  }

  viewing(): Promise<Viewing> {
    return this.get('/viewing');
  }

  records(): Promise<Records> {
    return this.get('/records');
  }

  sleep(): Promise<SleepState> {
    return this.get('/sleep');
  }

  async setSleep(bedtime: string, wake: string): Promise<SleepState> {
    const { revision } = await this.sleep();
    return this.send('put', '/sleep', { data: { bedtime, wake }, revision });
  }
}
