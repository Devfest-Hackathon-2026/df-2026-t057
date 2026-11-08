import { Injectable, computed } from '@angular/core';
import type { Resource, TimePolicyInput, Wallet } from '@dopagaki/contract';
import { ifMatch, requireData } from '../../core/http/api-client';
import { Operation, type Outcome } from '../../core/http/operation';
import { NotLoadedError } from '../../core/store/not-loaded';
import { ResourceStore } from '../../core/store/resource-store';

/** 視聴時間の残高とルール。 */
@Injectable({ providedIn: 'root' })
export class WalletStore extends ResourceStore<Wallet> {
  protected readonly resources: readonly Resource[] = ['wallet'];
  readonly savePolicy = new Operation(this.scope);

  /** 視聴枠を数えないルール（声かけだけ・自分で区切る）。表示を「制限なし」にする。 */
  readonly unlimited = computed(() => {
    const mode = this.value()?.policy.mode;
    return mode === 'reminder' || mode === 'self';
  });

  protected fetch(): Promise<Wallet> {
    return requireData(this.api.GET('/wallet'));
  }

  protected override revisionOf(value: Wallet): number {
    return value.revision;
  }

  updatePolicy(input: TimePolicyInput): Promise<Outcome<Wallet>> {
    return this.savePolicy.run(
      () => {
        const current = this.value();
        if (!current) throw new NotLoadedError('wallet');
        return requireData(
          this.api.PUT('/wallet/policy', {
            params: { header: ifMatch(current.revision) },
            body: input,
          }),
        );
      },
      (wallet) => {
        this.accept(wallet);
      },
    );
  }
}
