import { Injectable, signal } from '@angular/core';
import type { Resource, ShortcutStatus } from '@dopagaki/contract';
import { idempotent, requireData, requireOk } from '../../core/http/api-client';
import { Operation, type Outcome } from '../../core/http/operation';
import { ResourceStore } from '../../core/store/resource-store';

/** iPhone ショートカットの接続キー。 */
@Injectable({ providedIn: 'root' })
export class ShortcutStore extends ResourceStore<ShortcutStatus> {
  protected readonly resources: readonly Resource[] = ['shortcut'];
  readonly issue = new Operation(this.scope);
  readonly revoke = new Operation(this.scope);
  /** 発行したキー。応答でだけ受け取れるので、この画面を開いている間だけ持つ。 */
  readonly issuedKey = signal<string | undefined>(undefined);

  protected fetch(): Promise<ShortcutStatus> {
    return requireData(this.api.GET('/shortcut'));
  }

  protected override onReset(): void {
    this.issuedKey.set(undefined);
  }

  issueKey(): Promise<Outcome<{ key: string }>> {
    return this.issue.run(
      () => requireData(this.api.POST('/shortcut/key', { params: { header: idempotent() } })),
      ({ key }) => {
        this.issuedKey.set(key);
        void this.refresh();
      },
    );
  }

  revokeKey(): Promise<Outcome<void>> {
    return this.revoke.run(
      () => requireOk(this.api.DELETE('/shortcut/key', { params: { header: idempotent() } })),
      () => {
        this.issuedKey.set(undefined);
        void this.refresh();
      },
    );
  }

  forgetIssuedKey(): void {
    this.issuedKey.set(undefined);
  }
}
