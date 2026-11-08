import { Injectable, computed } from '@angular/core';
import type { Resource, Settings, SettingsInput } from '@dopagaki/contract';
import { ifMatch, requireData } from '../../core/http/api-client';
import { Operation, type Outcome } from '../../core/http/operation';
import { NotLoadedError } from '../../core/store/not-loaded';
import { ResourceStore } from '../../core/store/resource-store';

/** 表示と通知の設定。 */
@Injectable({ providedIn: 'root' })
export class SettingsStore extends ResourceStore<Settings> {
  protected readonly resources: readonly Resource[] = ['settings'];
  readonly save = new Operation(this.scope);

  /** 設定の「動きを控える」か、OS の視差効果を減らす設定。 */
  readonly reducedMotion = computed(
    () =>
      this.value()?.reducedMotion === true ||
      (typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches),
  );

  protected fetch(): Promise<Settings> {
    return requireData(this.api.GET('/settings'));
  }

  protected override revisionOf(value: Settings): number {
    return value.revision;
  }

  update(input: SettingsInput): Promise<Outcome<Settings>> {
    return this.save.run(
      () => {
        const current = this.value();
        if (!current) throw new NotLoadedError('settings');
        return requireData(
          this.api.PUT('/settings', { params: { header: ifMatch(current.revision) }, body: input }),
        );
      },
      (settings) => {
        this.accept(settings);
      },
    );
  }
}
