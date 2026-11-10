import { Injectable, computed, signal } from '@angular/core';
import type { CalendarEvent, CalendarStatus, Resource } from '@dopagaki/contract';
import { z } from 'zod';
import { idempotent, requireData } from '../../core/http/api-client';
import { Operation, type Outcome } from '../../core/http/operation';
import { ResourceStore } from '../../core/store/resource-store';

/** カレンダー画面に出す予定の種類。 */
export interface CalendarLayers {
  readonly tasks: boolean;
  readonly google: boolean;
}

const layersKey = 'dopagaki.calendarLayers';
const layersSchema = z.object({ tasks: z.boolean(), google: z.boolean() });

/** 表示の好みなので、ブラウザーごとに覚える。読めなければ両方とも表示する。 */
function readLayers(): CalendarLayers {
  try {
    const stored: unknown = JSON.parse(localStorage.getItem(layersKey) ?? 'null');
    return layersSchema.parse(stored);
  } catch {
    return { tasks: true, google: true };
  }
}

/** Google カレンダーとの連携状態と、主カレンダーの予定（表示だけ。保存しない）。 */
@Injectable({ providedIn: 'root' })
export class CalendarStore extends ResourceStore<CalendarStatus> {
  protected readonly resources: readonly Resource[] = ['calendar'];
  readonly connect = new Operation(this.scope);
  readonly sync = new Operation(this.scope);
  readonly disconnect = new Operation(this.scope);
  readonly reminders = new Operation(this.scope);
  readonly loadPrimary = new Operation(this.scope);

  readonly layers = signal<CalendarLayers>(readLayers());
  readonly primaryEvents = signal<readonly CalendarEvent[]>([]);
  /** 主カレンダーの予定を読める（連携していて、本人が読み取りを許可している）。 */
  readonly primaryReadable = computed(() => this.value()?.primaryEvents === true);

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

  protected override onReset(): void {
    this.primaryEvents.set([]);
  }

  setLayer(layer: keyof CalendarLayers, visible: boolean): void {
    this.layers.update((layers) => ({ ...layers, [layer]: visible }));
    try {
      localStorage.setItem(layersKey, JSON.stringify(this.layers()));
    } catch {
      // 保存できなくても、今の画面では切り替える。
    }
  }

  /** 表示している期間の、主カレンダーの予定を読む。 */
  loadPrimaryEvents(from: Date, to: Date): Promise<Outcome<CalendarEvent[]>> {
    return this.loadPrimary.run(
      () =>
        requireData(
          this.api.GET('/calendar/events', {
            params: { query: { from: from.toISOString(), to: to.toISOString() } },
          }),
        ),
      (events) => {
        this.primaryEvents.set(events);
      },
    );
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

  /** DOPAGAKI のカレンダーで、予定の開始の何分前に通知するかを保存する。 */
  saveReminders(minutes: readonly number[]): Promise<Outcome<CalendarStatus>> {
    return this.reminders.run(
      () =>
        requireData(
          this.api.PUT('/calendar/reminders', {
            params: { header: idempotent() },
            body: { minutes: [...minutes] },
          }),
        ),
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
