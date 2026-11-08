import { Injectable, computed } from '@angular/core';
import type { Morning, Resource, SleepInput, SleepState } from '@dopagaki/contract';
import { idempotent, ifMatch, requireData } from '../../core/http/api-client';
import { Operation, type Outcome } from '../../core/http/operation';
import { NotLoadedError } from '../../core/store/not-loaded';
import { ResourceStore } from '../../core/store/resource-store';

/** 就寝・起床の予定と、夜の時間帯。夜かどうかはサーバーが判定する。 */
@Injectable({ providedIn: 'root' })
export class SleepStore extends ResourceStore<SleepState> {
  protected readonly resources: readonly Resource[] = ['sleep'];
  readonly save = new Operation(this.scope);
  readonly clear = new Operation(this.scope);
  readonly morning = new Operation(this.scope);

  readonly sleep = computed(() => this.value()?.sleep ?? null);
  /** 今が夜の時間帯（就寝1時間前から起床まで）か。 */
  readonly night = computed(() => this.value()?.sleep?.night.active === true);

  protected fetch(): Promise<SleepState> {
    return requireData(this.api.GET('/sleep'));
  }

  protected override revisionOf(value: SleepState): number {
    return value.revision;
  }

  update(input: SleepInput): Promise<Outcome<SleepState>> {
    return this.save.run(
      () => {
        const current = this.value();
        if (!current) throw new NotLoadedError('sleep');
        return requireData(
          this.api.PUT('/sleep', { params: { header: ifMatch(current.revision) }, body: input }),
        );
      },
      (state) => {
        this.accept(state);
      },
    );
  }

  remove(): Promise<Outcome<SleepState>> {
    return this.clear.run(
      () => {
        const current = this.value();
        if (!current) throw new NotLoadedError('sleep');
        return requireData(
          this.api.DELETE('/sleep', { params: { header: ifMatch(current.revision) } }),
        );
      },
      (state) => {
        this.accept(state);
      },
    );
  }

  /** 前の夜を精算する。精算するものが無ければ null。 */
  settleMorning(): Promise<Outcome<Morning | null>> {
    return this.morning.run(async () => {
      const result = await requireData(
        this.api.POST('/sleep/morning', { params: { header: idempotent() } }),
      );
      return result.morning;
    });
  }
}
