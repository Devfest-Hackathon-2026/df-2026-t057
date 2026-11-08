import { DestroyRef, computed, inject, signal } from '@angular/core';
import type { Resource } from '@dopagaki/contract';
import { AccountScope } from '../auth/account-scope';
import { API_CLIENT } from '../http/api-client';
import { Operation, type Outcome } from '../http/operation';
import { RealtimeService } from '../realtime/realtime.service';

/**
 * 1つの API リソースを持つ store の共通部分。
 * - `ensureLoaded()` で初めて読み込み、以後は変更通知の版が新しければ取り直す。
 * - つなぎ直したら取り直す。アカウントが切り替わったら空に戻す。
 * - 取り直しが重なったら、実行中の1回が終わってからもう1回だけ実行する。
 */
export abstract class ResourceStore<T> {
  protected readonly api = inject(API_CLIENT);
  protected readonly scope = inject(AccountScope);
  protected readonly realtime = inject(RealtimeService);

  /** 変更通知を受け取るリソース。最初の要素が、本文の `revision` に対応する。 */
  protected abstract readonly resources: readonly Resource[];

  readonly value = signal<T | undefined>(undefined);
  readonly loaded = computed(() => this.value() !== undefined);
  /** 読み込み（取り直し）の通信状態とエラー。 */
  readonly load = new Operation(this.scope);

  private readonly held = new Map<Resource, number>();
  private bodyRevision = -1;
  private requested = false;
  private inFlight: Promise<Outcome<T>> | undefined;
  private again = false;

  constructor() {
    const changes = this.realtime.changes$.subscribe((event) => {
      if (!this.requested || !this.resources.includes(event.resource)) return;
      if (event.revision > (this.held.get(event.resource) ?? -1)) void this.refresh();
    });
    const reconnects = this.realtime.reconnected$.subscribe(() => {
      if (this.requested) void this.refresh();
    });
    this.scope.onReset(() => {
      this.requested = false;
      this.inFlight = undefined;
      this.again = false;
      this.held.clear();
      this.bodyRevision = -1;
      this.value.set(undefined);
      this.onReset();
    });
    inject(DestroyRef).onDestroy(() => {
      changes.unsubscribe();
      reconnects.unsubscribe();
    });
  }

  /** 取得する。 */
  protected abstract fetch(): Promise<T>;

  /** 本文に版（`revision`）があれば返す。 */
  protected revisionOf(value: T): number | undefined {
    return typeof value === 'object' &&
      value !== null &&
      'revision' in value &&
      typeof value.revision === 'number'
      ? value.revision
      : undefined;
  }

  /** アカウントが切り替わったときに、派生した状態を消す。 */
  protected onReset(): void {
    // 既定では何もしない。
  }

  /** まだ読み込んでいなければ読み込む。 */
  ensureLoaded(): void {
    if (!this.requested) void this.refresh();
  }

  /** 取り直す。実行中なら、終わってからもう一度だけ取り直す。 */
  refresh(): Promise<Outcome<T>> {
    this.requested = true;
    if (this.inFlight) {
      this.again = true;
      return this.inFlight;
    }
    const run = async (): Promise<Outcome<T>> => {
      let outcome: Outcome<T>;
      do {
        this.again = false;
        const snapshot = this.resources.map(
          (resource) => [resource, this.realtime.latestRevision(resource) ?? -1] as const,
        );
        outcome = await this.load.run(
          () => this.fetch(),
          (value) => {
            this.accept(value);
            for (const [resource, revision] of snapshot) this.hold(resource, revision);
          },
        );
      } while (this.repeatRequested() && this.inFlight === current);
      return outcome;
    };
    const current = run().finally(() => {
      if (this.inFlight === current) this.inFlight = undefined;
    });
    this.inFlight = current;
    return current;
  }

  /** 操作の応答などで受け取った最新の値を反映する。 */
  accept(value: T): void {
    this.requested = true;
    const revision = this.revisionOf(value);
    const primary = this.resources[0];
    if (revision !== undefined && primary) {
      // 古い応答で新しい値を上書きしない。
      if (revision < this.bodyRevision && this.value() !== undefined) return;
      this.bodyRevision = revision;
      this.hold(primary, revision);
    }
    this.value.set(value);
  }

  /** 実行中に取り直しを頼まれたか（await をはさむので、メソッドで読み直す）。 */
  private repeatRequested(): boolean {
    return this.again;
  }

  private hold(resource: Resource, revision: number): void {
    this.held.set(resource, Math.max(this.held.get(resource) ?? -1, revision));
  }
}
