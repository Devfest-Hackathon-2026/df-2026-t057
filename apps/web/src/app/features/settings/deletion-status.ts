import { ChangeDetectionStrategy, Component, signal } from '@angular/core';
import { createApiClient } from '@dopagaki/contract';
import { detectLocale, errorMessage, messages } from '@dopagaki/messages';
// 退会してログアウトした後に開くページなので、プロフィールの言語ではなくブラウザーの言語で表示する。
const locale = detectLocale(navigator.languages);
@Component({
  selector: 'app-root',
  template:
    '<main><h1>{{text.heading}}</h1><p role="status">{{message()}}</p><button type="button" (click)="check()" [disabled]="pending()">{{text.refresh}}</button></main>',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class DeletionStatus {
  protected readonly text = messages[locale].web.settings.deletionStatus;
  readonly message = signal(this.text.checking);
  readonly pending = signal(false);
  private readonly receipt = location.hash.slice(1);
  constructor() {
    document.documentElement.lang = locale;
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
        result.data?.status === 'complete' ? this.text.complete : this.text.accepted,
      );
    } catch (e: unknown) {
      this.message.set(errorMessage(e, messages[locale].shared));
    } finally {
      this.pending.set(false);
    }
  }
}
