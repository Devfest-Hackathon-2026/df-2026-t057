import { Injectable, computed, inject } from '@angular/core';
import type {
  Draft,
  Plan,
  Resource,
  Task,
  TaskCreateInput,
  TaskInput,
  TaskList,
  WorkRecord,
  WorkSession,
} from '@dopagaki/contract';
import { idempotent, ifMatch, requireData, requireOk } from '../../core/http/api-client';
import { Operation, type Outcome } from '../../core/http/operation';
import { ResourceStore } from '../../core/store/resource-store';
import { WalletStore } from '../wallet/wallet.store';
import { WorkSessionStore } from '../work/work-session.store';

export type DraftMode = 'snooze' | 'adjust';

export interface DraftRequest {
  readonly mode: DraftMode;
  /** この時刻から始める案にする。指定するときは `revision` も必要。 */
  readonly start?: string;
  readonly revision?: number;
}

/** 課題の一覧・計画の版・次のスヌーズ案と、課題と計画の操作。 */
@Injectable({ providedIn: 'root' })
export class TasksStore extends ResourceStore<TaskList> {
  protected readonly resources: readonly Resource[] = ['tasks'];
  private readonly wallet = inject(WalletStore);
  private readonly work = inject(WorkSessionStore);

  readonly create = new Operation(this.scope);
  readonly update = new Operation(this.scope);
  readonly remove = new Operation(this.scope);
  readonly start = new Operation(this.scope);
  readonly complete = new Operation(this.scope);
  readonly extract = new Operation(this.scope);
  readonly previewNew = new Operation(this.scope);
  readonly previewEdit = new Operation(this.scope);
  readonly draft = new Operation(this.scope);
  readonly adjust = new Operation(this.scope);
  readonly commit = new Operation(this.scope);
  readonly sample = new Operation(this.scope);

  readonly tasks = computed(() => this.value()?.items ?? []);
  /** 計画の版。変更案やカードの版と比べて、古くなったかを判断する。 */
  readonly revision = computed(() => this.value()?.revision);
  readonly nextSnooze = computed(() => this.value()?.nextSnooze ?? null);
  /** 次に取りかかる課題（未開始で、予定の開始が一番早いもの）。 */
  readonly next = computed(() => {
    let next: Task | undefined;
    for (const task of this.tasks()) {
      if (task.status !== 'pending') continue;
      if (!next || Date.parse(task.plan.start) < Date.parse(next.plan.start)) next = task;
    }
    return next;
  });

  protected fetch(): Promise<TaskList> {
    return requireData(this.api.GET('/tasks'));
  }

  protected override revisionOf(value: TaskList): number {
    return value.revision;
  }

  find(taskId: string): Task | undefined {
    return this.tasks().find((task) => task.id === taskId);
  }

  /** 課題を登録する。相談のカードから登録するときは、カードの版を `revision` に渡す。 */
  createTask(input: TaskCreateInput, revision?: number): Promise<Outcome<Task>> {
    return this.create.run(
      () =>
        requireData(
          this.api.POST('/tasks', {
            params: { header: revision === undefined ? idempotent() : ifMatch(revision) },
            body: input,
          }),
        ),
      () => {
        void this.refresh();
        void this.wallet.refresh();
      },
    );
  }

  /** 開始前の課題の内容を変更する。`revision` は変更案の元にした計画の版。 */
  updateTask(taskId: string, input: TaskInput, revision: number): Promise<Outcome<Task>> {
    return this.update.run(
      () =>
        requireData(
          this.api.PUT('/tasks/{taskId}', {
            params: { path: { taskId }, header: ifMatch(revision) },
            body: input,
          }),
        ),
      () => {
        void this.refresh();
      },
    );
  }

  deleteTask(taskId: string): Promise<Outcome<void>> {
    return this.remove.run(
      () =>
        requireOk(
          this.api.DELETE('/tasks/{taskId}', {
            params: { path: { taskId }, header: idempotent() },
          }),
        ),
      () => {
        void this.refresh();
      },
    );
  }

  startTask(taskId: string): Promise<Outcome<WorkSession>> {
    return this.start.run(
      () =>
        requireData(
          this.api.POST('/tasks/{taskId}/start', {
            params: { path: { taskId }, header: idempotent() },
          }),
        ),
      (session) => {
        this.work.accept(session);
        void this.refresh();
      },
    );
  }

  completeTask(taskId: string): Promise<Outcome<{ task: Task; work?: WorkRecord }>> {
    return this.complete.run(
      () =>
        requireData(
          this.api.POST('/tasks/{taskId}/complete', {
            params: { path: { taskId }, header: idempotent() },
          }),
        ),
      () => {
        void this.refresh();
        void this.wallet.refresh();
        void this.work.refresh();
      },
    );
  }

  /** 自由な文章から課題の候補を取り出す（保存しない）。 */
  extractTasks(text: string): Promise<Outcome<TaskInput[]>> {
    return this.extract.run(async () => {
      const result = await requireData(
        this.api.POST('/tasks/extract', { params: { header: idempotent() }, body: { text } }),
      );
      return result.tasks;
    });
  }

  /** 新しい課題を登録した場合の計画（保存しない）。 */
  previewNewTask(input: TaskInput): Promise<Outcome<{ plan: Plan; revision: number }>> {
    return this.previewNew.run(() =>
      requireData(
        this.api.POST('/tasks/preview', { params: { header: idempotent() }, body: input }),
      ),
    );
  }

  /** 課題の内容を変えた場合の変更案（保存しない）。 */
  previewTaskEdit(taskId: string, input: TaskInput): Promise<Outcome<Draft>> {
    return this.previewEdit.run(() =>
      requireData(
        this.api.POST('/tasks/{taskId}/edit-preview', {
          params: { path: { taskId }, header: idempotent() },
          body: input,
        }),
      ),
    );
  }

  /** スヌーズ・調整の変更案（保存しない）。 */
  rescheduleDraft(taskId: string, request: DraftRequest): Promise<Outcome<Draft>> {
    const query: { mode: DraftMode; start?: string; revision?: number } = { mode: request.mode };
    if (request.start !== undefined) query.start = request.start;
    if (request.revision !== undefined) query.revision = request.revision;
    return this.draft.run(() =>
      requireData(
        this.api.GET('/tasks/{taskId}/reschedule', { params: { path: { taskId }, query } }),
      ),
    );
  }

  /** 変更案を指示に合わせて AI で調整する（保存しない）。 */
  adjustDraft(draft: Draft, instruction: string): Promise<Outcome<Draft>> {
    return this.adjust.run(() =>
      requireData(
        this.api.POST('/reschedule/adjust', {
          params: { header: idempotent() },
          body: { draft, instruction },
        }),
      ),
    );
  }

  /** 変更案を確定する。本人が選んだ時刻をそのまま保存する。 */
  commitDraft(draft: Draft): Promise<Outcome<TaskList>> {
    return this.commit.run(
      () =>
        requireData(
          this.api.POST('/reschedule/commit', {
            params: { header: ifMatch(draft.baseRevision) },
            body: {
              kind: draft.kind,
              changes: draft.changes.map(({ taskId, after }) => ({ taskId, after })),
            },
          }),
        ),
      (list) => {
        this.accept(list);
      },
    );
  }

  /** デモで、サンプルの課題を3件追加する。 */
  addSample(): Promise<Outcome<TaskList>> {
    return this.sample.run(
      () => requireData(this.api.POST('/demo/sample', { params: { header: idempotent() } })),
      (list) => {
        this.accept(list);
        void this.wallet.refresh();
      },
    );
  }
}
