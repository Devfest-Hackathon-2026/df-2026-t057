import { Injectable, computed, effect, signal, untracked } from '@angular/core';
import type { Resource, WorkRecord, WorkSession } from '@dopagaki/contract';
import { idempotent, requireData } from '../../core/http/api-client';
import { Operation, type Outcome } from '../../core/http/operation';
import { NotLoadedError } from '../../core/store/not-loaded';
import { ResourceStore } from '../../core/store/resource-store';
import { displayedElapsedSeconds, workProgress } from './work-timer';

/** 実行中の作業（無ければ null）と、1秒ごとに進む表示用の経過時間。 */
@Injectable({ providedIn: 'root' })
export class WorkSessionStore extends ResourceStore<WorkSession | null> {
  protected readonly resources: readonly Resource[] = ['workSession'];
  readonly pause = new Operation(this.scope);
  readonly resume = new Operation(this.scope);
  readonly confirm = new Operation(this.scope);

  /** `asOf` に当たる、この端末の時刻。 */
  private readonly receivedAt = signal(Date.now());
  private readonly now = signal(Date.now());
  private ticker: ReturnType<typeof setInterval> | undefined;

  readonly session = computed(() => this.value() ?? null);
  readonly elapsedSeconds = computed(() => {
    const session = this.session();
    return session ? displayedElapsedSeconds(session, this.receivedAt(), this.now()) : 0;
  });
  readonly progress = computed(() => {
    const session = this.session();
    return session ? workProgress(this.elapsedSeconds(), session.limitSeconds) : 0;
  });
  /** 記録できる上限（分）。 */
  readonly limitMinutes = computed(() => Math.ceil((this.session()?.limitSeconds ?? 0) / 60));

  constructor() {
    super();
    effect(() => {
      const running = this.session()?.running === true;
      untracked(() => {
        this.tick(running);
      });
    });
  }

  protected fetch(): Promise<WorkSession | null> {
    return requireData(this.api.GET('/work-session')).then((result) => result.session);
  }

  override accept(value: WorkSession | null): void {
    this.receivedAt.set(Date.now());
    this.now.set(Date.now());
    super.accept(value);
  }

  pauseWork(): Promise<Outcome<WorkSession>> {
    return this.pause.run(
      () => requireData(this.api.POST('/work-session/pause', { params: { header: idempotent() } })),
      (session) => {
        this.accept(session);
      },
    );
  }

  resumeWork(): Promise<Outcome<WorkSession>> {
    return this.resume.run(
      () =>
        requireData(this.api.POST('/work-session/resume', { params: { header: idempotent() } })),
      (session) => {
        this.accept(session);
      },
    );
  }

  /** ここまでの作業を記録する。課題は未開始に戻る。 */
  confirmWork(): Promise<Outcome<WorkRecord>> {
    return this.confirm.run(
      () => {
        const session = this.session();
        if (!session) throw new NotLoadedError('workSession');
        return requireData(
          this.api.POST('/work-session/confirm', {
            params: { header: idempotent() },
            body: { sessionId: session.id },
          }),
        );
      },
      () => {
        this.accept(null);
      },
    );
  }

  private tick(running: boolean): void {
    if (running && this.ticker === undefined) {
      this.ticker = setInterval(() => {
        this.now.set(Date.now());
      }, 1000);
    } else if (!running && this.ticker !== undefined) {
      clearInterval(this.ticker);
      this.ticker = undefined;
    }
  }
}
