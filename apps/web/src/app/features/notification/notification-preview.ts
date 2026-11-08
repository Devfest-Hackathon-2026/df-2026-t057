import {
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  inject,
  signal,
  untracked,
} from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MAT_DIALOG_DATA, MatDialogModule, MatDialogRef } from '@angular/material/dialog';
import { MatIconModule } from '@angular/material/icon';
import { LocaleStore } from '../../core/i18n/locale.store';
import { CompanionService } from '../companion/companion.service';
import { SettingsStore } from '../settings/settings.store';
import { TaskActions } from '../tasks/task-actions';
import { TasksStore } from '../tasks/tasks.store';
import { WalletStore } from '../wallet/wallet.store';
import { PlanMinutesPipe, TimePipe } from '../../shared/ui/format';
import type { NotificationPreviewData } from './notification-preview.model';

/** 通知に気づけるよう、短い音を一度だけ鳴らす。鳴らせない環境（自動再生の制限など）では何もしない。 */
function playChime(): void {
  try {
    const context = new AudioContext();
    const oscillator = context.createOscillator();
    const gain = context.createGain();
    oscillator.frequency.value = 880;
    gain.gain.setValueAtTime(0.0001, context.currentTime);
    gain.gain.exponentialRampToValueAtTime(0.15, context.currentTime + 0.02);
    gain.gain.exponentialRampToValueAtTime(0.0001, context.currentTime + 0.4);
    oscillator.connect(gain).connect(context.destination);
    oscillator.start();
    oscillator.stop(context.currentTime + 0.45);
    oscillator.onended = () => {
      void context.close();
    };
  } catch {
    // 音は補助。鳴らせなくても通知は表示する。
  }
}

/** 通知のプレビュー（拡張が YouTube の上に出す通知と同じ内容）。 */
@Component({
  selector: 'app-notification-preview',
  imports: [MatButtonModule, MatDialogModule, MatIconModule, PlanMinutesPipe, TimePipe],
  templateUrl: './notification-preview.html',
  styleUrl: './notification-preview.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: {
    '[class.pachinko]': "mode() === 'pachinko'",
    '[class.mahjong]': "mode() === 'mahjong'",
    '[class.simple]': "mode() === 'simple'",
    '[class.reduced]': 'settings.reducedMotion()',
  },
})
export class NotificationPreview {
  private readonly data = inject<NotificationPreviewData>(MAT_DIALOG_DATA);
  private readonly ref = inject(MatDialogRef<NotificationPreview>);
  private readonly tasks = inject(TasksStore);
  private readonly wallet = inject(WalletStore);
  private readonly companion = inject(CompanionService);
  protected readonly settings = inject(SettingsStore);
  protected readonly actions = inject(TaskActions);
  protected readonly text = inject(LocaleStore).web;

  protected readonly task = computed(() => this.tasks.find(this.data.taskId));
  protected readonly mode = computed(() => this.settings.value()?.mode ?? 'pachinko');
  protected readonly heading = computed(() =>
    this.mode() === 'mahjong'
      ? this.text().notification.preview.headingMahjong
      : this.text().notification.preview.heading,
  );
  protected readonly firstStep = computed(() => {
    const task = this.task();
    const step = task?.plan.steps[0];
    return step && step.title !== task.title ? step.title : undefined;
  });
  protected readonly reward = computed(() => {
    const wallet = this.wallet.value();
    if (wallet?.policy.mode !== 'rewards') return this.text().notification.preview.noReward;
    const work = Math.round(wallet.rules.workSecondsPerReward / 60);
    const reward = Math.round(wallet.rules.rewardSecondsPerUnit / 60);
    return this.text().notification.preview.reward(work, reward);
  });
  protected readonly companionLine = signal<string | undefined>(undefined);
  protected readonly starting = this.tasks.start.pending;
  /** このプレビューから開始を押したか。ほかの画面で起きた古い失敗は出さない。 */
  private readonly startAttempted = signal(false);
  protected readonly startError = computed(() =>
    this.startAttempted() ? this.tasks.start.error() : undefined,
  );

  constructor() {
    const taskId = this.data.taskId;
    void this.companion.line('notice', taskId).then((line) => {
      // 表示中の課題が変わっていたら出さない。
      if (this.data.taskId === taskId) this.companionLine.set(line);
    });
    let chimed = false;
    effect(() => {
      const sound = this.settings.value()?.sound === true;
      if (!sound || chimed) return;
      chimed = true;
      untracked(playChime);
    });
  }

  protected start(): void {
    const task = this.task();
    if (!task) return;
    this.startAttempted.set(true);
    void this.actions.start(task);
  }

  protected snooze(): void {
    const task = this.task();
    if (task) void this.actions.plan(task, 'snooze');
  }

  protected adjust(): void {
    const task = this.task();
    if (task) void this.actions.plan(task, 'adjust');
  }

  protected later(): void {
    this.ref.close();
  }
}
