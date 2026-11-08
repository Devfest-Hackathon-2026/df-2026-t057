import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatProgressSpinnerModule } from '@angular/material/progress-spinner';
import { AuthService } from '../../core/auth/auth.service';
import { ConfigStore } from '../../core/config/config.store';
import { LocaleStore } from '../../core/i18n/locale.store';

/** ログイン前の画面（live）。設定を読み込む間もここを出す。 */
@Component({
  selector: 'app-login-screen',
  imports: [MatButtonModule, MatProgressSpinnerModule],
  templateUrl: './login-screen.html',
  styleUrl: './login-screen.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class LoginScreen {
  protected readonly auth = inject(AuthService);
  protected readonly config = inject(ConfigStore);
  protected readonly text = inject(LocaleStore).web;
  protected readonly initializing = computed(() => this.auth.status() === 'initializing');
  protected readonly signingIn = computed(() => this.auth.status() === 'signingIn');
  /** ログインを押せるのは、ログインしていない状態が確定したときだけ。 */
  protected readonly loginDisabled = computed(() => this.auth.status() !== 'signedOut');
  protected readonly loginLabel = computed(() => {
    const text = this.text().login.screen;
    if (this.signingIn()) return text.signingIn;
    return this.initializing() ? text.loading : text.signIn;
  });
}
