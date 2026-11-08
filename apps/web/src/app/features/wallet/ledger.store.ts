import { Injectable, signal } from '@angular/core';
import type { LedgerEntry, Resource } from '@dopagaki/contract';
import { requireData } from '../../core/http/api-client';
import { Operation } from '../../core/http/operation';
import { ResourceStore } from '../../core/store/resource-store';

interface LedgerPage {
  readonly entries: readonly LedgerEntry[];
  readonly next: string | undefined;
}

/** 視聴時間の台帳（新しい順）。残高が変わったら最初のページを取り直す。 */
@Injectable({ providedIn: 'root' })
export class LedgerStore extends ResourceStore<LedgerPage> {
  protected readonly resources: readonly Resource[] = ['wallet'];
  readonly more = new Operation(this.scope);
  /** 「もっと見る」で読み足した項目。 */
  readonly older = signal<readonly LedgerEntry[]>([]);
  private readonly cursor = signal<string | undefined>(undefined);

  protected async fetch(): Promise<LedgerPage> {
    const page = await requireData(
      this.api.GET('/wallet/ledger', { params: { query: { limit: 100 } } }),
    );
    this.older.set([]);
    return { entries: page.entries, next: page.next };
  }

  protected override onReset(): void {
    this.older.set([]);
  }

  /** 次のページを読み足す。 */
  async loadMore(): Promise<void> {
    const before = this.nextCursor();
    if (!before) return;
    await this.more.run(
      () =>
        requireData(this.api.GET('/wallet/ledger', { params: { query: { limit: 100, before } } })),
      (page) => {
        this.older.update((entries) => [...entries, ...page.entries]);
        this.cursor.set(page.next);
      },
    );
  }

  private nextCursor(): string | undefined {
    return this.older().length > 0 ? this.cursor() : this.value()?.next;
  }

  /** 次のページがあるか。 */
  hasMore(): boolean {
    return this.nextCursor() !== undefined;
  }
}
