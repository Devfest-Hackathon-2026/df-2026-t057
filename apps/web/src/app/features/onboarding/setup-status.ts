import { AccountScope } from '../../core/auth/account-scope';
import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { AuthService } from '../../core/auth/auth.service';
import { ExtensionBridge } from '../../core/extension/extension-bridge';
import { LocaleStore } from '../../core/i18n/locale.store';
import { CalendarStore } from '../calendar/calendar.store';
@Component({
  selector: 'app-setup-status',
  imports: [MatButtonModule],
  template: `
    @let t = text().onboarding.setupStatus;
    <h3>{{ t.heading }}</h3>
    <ol>
      <li>{{ t.signedIn(auth.signedIn()) }}</li>
      <li>{{ t.calendar(calendar.connected()) }}</li>
      <li>{{ t.extension(bridge.extensionVersion()) }}</li>
      <li>{{ t.sameAccount(sameAccount()) }}</li>
      <li>{{ t.notifications(bridge.diagnostic()?.notifications === 'granted') }}</li>
    </ol>
    <p>{{ t.instructions }}</p>
    <button
      mat-stroked-button
      type="button"
      (click)="bridge.diagnose()"
      [disabled]="bridge.diagnosing()"
    >
      {{ bridge.diagnosing() ? t.diagnosing : t.diagnose }}
    </button>
    <button
      mat-button
      type="button"
      (click)="seen.set(true)"
      [disabled]="bridge.diagnostic()?.notifications !== 'granted'"
    >
      {{ t.confirmTest }}
    </button>
    <p aria-live="polite">{{ seen() ? t.testConfirmed : '' }}</p>
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SetupStatus {
  constructor() {
    inject(AccountScope).onReset(() => {
      this.seen.set(false);
    });
  }
  protected readonly text = inject(LocaleStore).web;
  protected readonly auth = inject(AuthService);
  protected readonly bridge = inject(ExtensionBridge);
  protected readonly calendar = inject(CalendarStore);
  protected readonly seen = signal(false);
  protected readonly sameAccount = computed(() => {
    const d = this.bridge.diagnostic();
    return (
      d?.connected === true &&
      (this.auth.mode() === 'demo' || d.accountId === this.auth.account()?.id)
    );
  });
}
