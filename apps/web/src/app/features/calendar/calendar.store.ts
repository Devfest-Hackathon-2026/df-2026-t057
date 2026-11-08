import { Injectable, computed } from '@angular/core';
import type { CalendarStatus, Resource } from '@dopagaki/contract';
import { idempotent, requireData } from '../../core/http/api-client';
import { Operation, type Outcome } from '../../core/http/operation';
import { ResourceStore } from '../../core/store/resource-store';

/** Google カレンダーとの連携状態。 */
@Injectable({ providedIn: 'root' })
export class CalendarStore extends ResourceStore<CalendarStatus> {
  protected readonly resources: readonly Resource[] = ['calendar'];
  readonly connect = new Operation(this.scope);
  readonly sync = new Operation(this.scope);
  readonly disconnect = new Operation(this.scope);

  readonly connected = computed(() => this.value()?.connected === true);
  /** 管理画面の変更を Google へ反映できていない。 */
  readonly needsRetry = computed(() => {
    const status = this.value();
    return (
      status !== undefined &&
      status.connected &&
      (status.sync === 'pending' || status.sync === 'failed')
    );
  });

  protected fetch(): Promise<CalendarStatus> {
    return requireData(this.api.GET('/calendar'));
  }

  /** Google の同意画面へ移動する。 */
  startConnect(): Promise<Outcome<{ url: string }>> {
    return this.connect.run(
      () => requireData(this.api.POST('/calendar/connect', { params: { header: idempotent() } })),
      ({ url }) => {
        location.assign(url);
      },
    );
  }

  syncNow(): Promise<Outcome<CalendarStatus>> {
    return this.sync.run(
      () => requireData(this.api.POST('/calendar/sync', { params: { header: idempotent() } })),
      (status) => {
        this.accept(status);
      },
    );
  }

  /** 連携を解除する。課題は残り、カレンダーの予定との結び付きだけが外れる。 */
  disconnectNow(): Promise<Outcome<CalendarStatus>> {
    return this.disconnect.run(
      () =>
        requireData(this.api.POST('/calendar/disconnect', { params: { header: idempotent() } })),
      (status) => {
        this.accept(status);
      },
    );
  }
}
