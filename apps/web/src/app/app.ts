import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { AuthService } from './core/auth/auth.service';
import { ExtensionBridge } from './core/extension/extension-bridge';
import { RealtimeService } from './core/realtime/realtime.service';
import { LoginScreen } from './features/login/login-screen';
import { Shell } from './features/shell/shell';

@Component({
  selector: 'app-root',
  imports: [LoginScreen, Shell],
  template: `
    @if (auth.signedIn()) {
      <app-shell />
    } @else {
      <app-login-screen />
    }
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class App {
  protected readonly auth = inject(AuthService);

  constructor() {
    // 変更通知と拡張との受け渡しは、ログインの状態に合わせて自分でつなぐ・切る。
    inject(RealtimeService);
    inject(ExtensionBridge);
    void this.auth.start();
  }
}
