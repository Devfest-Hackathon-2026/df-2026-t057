import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';
import { MatProgressSpinnerModule } from '@angular/material/progress-spinner';
import { AuthService } from '../../core/auth/auth.service';
import { ConfigStore } from '../../core/config/config.store';

/** ログイン前の画面（live）。設定を読み込む間もここを出す。 */
@Component({
  selector: 'app-login-screen',
  imports: [MatButtonModule, MatIconModule, MatProgressSpinnerModule],
  templateUrl: './login-screen.html',
  styleUrl: './login-screen.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class LoginScreen {
  protected readonly auth = inject(AuthService);
  protected readonly config = inject(ConfigStore);
  /** 使い方のサンプルで「完了」を押したか（表示だけ）。 */
  protected readonly completed = signal(false);

  protected readonly initializing = computed(() => this.auth.status() === 'initializing');
  protected readonly signingIn = computed(() => this.auth.status() === 'signingIn');
  /** ログインを押せるのは、ログインしていない状態が確定したときだけ。 */
  protected readonly loginDisabled = computed(() => this.auth.status() !== 'signedOut');
  protected readonly loginLabel = computed(() =>
    this.signingIn() ? 'ログイン中…' : this.initializing() ? '読み込み中…' : 'Googleでログイン',
  );
}
