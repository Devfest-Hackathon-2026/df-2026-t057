import { ChangeDetectionStrategy, Component, signal } from '@angular/core';
import { createApiClient } from '@dopagaki/contract';
import { errorMessage } from '@dopagaki/messages';
@Component({
  selector: 'app-root',
  template:
    '<main><h1>退会処理の状況</h1><p role="status">{{message()}}</p><button type="button" (click)="check()" [disabled]="pending()">更新</button></main>',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class DeletionStatus {
  readonly message = signal('確認しています…');
  readonly pending = signal(false);
  private readonly receipt = location.hash.slice(1);
  constructor() {
    void this.check();
  }
  async check(): Promise<void> {
    this.pending.set(true);
    try {
      const api = createApiClient({ origin: location.origin, authorization: () => undefined });
      const result = await api.POST('/account/deletion-status', {
        body: { receipt: this.receipt },
      });
      this.message.set(
        result.data?.status === 'complete'
          ? '退会処理が完了しました。'
          : '削除を受け付けました。処理が完了するまでお待ちください。',
      );
    } catch (e: unknown) {
      this.message.set(errorMessage(e));
    } finally {
      this.pending.set(false);
    }
  }
}
