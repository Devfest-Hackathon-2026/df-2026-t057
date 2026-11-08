import { AccountScope } from '../../core/auth/account-scope';
import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { AuthService } from '../../core/auth/auth.service';
import { ExtensionBridge } from '../../core/extension/extension-bridge';
import { CalendarStore } from '../calendar/calendar.store';
@Component({
  selector: 'app-setup-status',
  imports: [MatButtonModule],
  template: `
    <h3>導入と接続の確認</h3>
    <ol>
      <li>管理画面へログイン：{{ auth.signedIn() ? '完了' : '未完了' }}</li>
      <li>Googleカレンダー：{{ calendar.connected() ? '連携済み' : '未連携' }}</li>
      <li>拡張：{{ bridge.extensionVersion() ?? '未検出' }}</li>
      <li>同じアカウントで接続：{{ sameAccount() ? '確認済み' : '未確認' }}</li>
      <li>
        通知：{{
          bridge.diagnostic()?.notifications === 'granted' ? '許可済み' : '未確認または拒否'
        }}
      </li>
    </ol>
    <p>Chromeの拡張ポップアップでGoogle接続と通知テストを実行してください。</p>
    <button
      mat-stroked-button
      type="button"
      (click)="bridge.diagnose()"
      [disabled]="bridge.diagnosing()"
    >
      {{ bridge.diagnosing() ? '確認中…' : '接続を診断' }}
    </button>
    <button
      mat-button
      type="button"
      (click)="seen.set(true)"
      [disabled]="bridge.diagnostic()?.notifications !== 'granted'"
    >
      テスト通知を確認した
    </button>
    <p aria-live="polite">{{ seen() ? '通知を本人が確認しました。' : '' }}</p>
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SetupStatus {
  constructor() {
    inject(AccountScope).onReset(() => {
      this.seen.set(false);
    });
  }
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
