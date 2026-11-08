import {
  ChangeDetectionStrategy,
  Component,
  DestroyRef,
  computed,
  effect,
  inject,
  signal,
  untracked,
} from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { Clipboard } from '@angular/cdk/clipboard';
import {
  FormControl,
  FormGroup,
  ReactiveFormsModule,
  Validators,
  type ValidatorFn,
} from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatButtonToggleModule } from '@angular/material/button-toggle';
import { MatDialog } from '@angular/material/dialog';
import { MatExpansionModule } from '@angular/material/expansion';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { MatSelectModule } from '@angular/material/select';
import { MatSlideToggleModule } from '@angular/material/slide-toggle';
import { MatSnackBar } from '@angular/material/snack-bar';
import type { NoticeMode, Profile, Settings, TimePolicyMode } from '@dopagaki/contract';
import { durationLabel, timePolicyModeLabels } from '@dopagaki/messages';
import { firstValueFrom } from 'rxjs';
import { AuthService } from '../../../core/auth/auth.service';
import { ConfigStore } from '../../../core/config/config.store';
import { DateTimePipe } from '../../../shared/ui/format';
import { PageHeading } from '../../../shared/ui/page-heading';
import { CalendarStore } from '../../calendar/calendar.store';
import { ShortcutStore } from '../../shortcut/shortcut.store';
import { SleepStore } from '../../sleep/sleep.store';
import { TasksStore } from '../../tasks/tasks.store';
import { WalletStore } from '../../wallet/wallet.store';
import { WorkSessionStore } from '../../work/work-session.store';
import { ProfileStore } from '../profile.store';
import { SettingsStore } from '../settings.store';
import { DisconnectCalendarDialog } from './disconnect-calendar-dialog';

type DailyLimit = 600 | 1200 | 1800;

interface MainValue {
  readonly mode: NoticeMode;
  readonly snoozeMinutes: number;
  readonly sound: boolean;
  readonly reducedMotion: boolean;
  readonly grime: boolean;
  readonly name: string;
  readonly context: string;
}

interface PolicyValue {
  readonly mode: TimePolicyMode;
  readonly dailyLimitSeconds: DailyLimit;
}

interface SleepValue {
  readonly bedtime: string;
  readonly wake: string;
}

/** Validators.required をメソッドとして切り離さずに使う。 */
const required: ValidatorFn = (control) => Validators.required(control);

const settingKeys = ['mode', 'snoozeMinutes', 'sound', 'reducedMotion', 'grime'] as const;
const profileKeys = ['name', 'context'] as const;

function sameKeys<K extends string>(
  a: Readonly<Record<K, unknown>>,
  b: Readonly<Record<K, unknown>>,
  keys: readonly K[],
): boolean {
  return keys.every((key) => a[key] === b[key]);
}

function toDailyLimit(seconds: number): DailyLimit {
  return seconds === 600 || seconds === 1200 ? seconds : 1800;
}

const syncStatusLabels = {
  disconnected: '未連携',
  synced: '同期済み',
  pending: '反映待ち',
  failed: '反映に失敗',
} as const;

/** 設定。各まとまりは自分の操作の通信状態とエラーを持つ。 */
@Component({
  selector: 'app-settings-page',
  imports: [
    DateTimePipe,
    MatButtonModule,
    MatButtonToggleModule,
    MatExpansionModule,
    MatFormFieldModule,
    MatIconModule,
    MatInputModule,
    MatSelectModule,
    MatSlideToggleModule,
    PageHeading,
    ReactiveFormsModule,
  ],
  templateUrl: './settings-page.html',
  styleUrl: './settings-page.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SettingsPage {
  protected readonly auth = inject(AuthService);
  protected readonly config = inject(ConfigStore);
  protected readonly settings = inject(SettingsStore);
  protected readonly profile = inject(ProfileStore);
  protected readonly wallet = inject(WalletStore);
  protected readonly sleepBonus = computed(() =>
    durationLabel(this.wallet.value()?.rules.sleepBonusSeconds ?? 900),
  );
  protected readonly sleep = inject(SleepStore);
  protected readonly shortcut = inject(ShortcutStore);
  protected readonly calendar = inject(CalendarStore);
  protected readonly work = inject(WorkSessionStore);
  private readonly tasks = inject(TasksStore);
  private readonly snackBar = inject(MatSnackBar);
  private readonly clipboard = inject(Clipboard);
  private readonly dialog = inject(MatDialog);

  protected readonly modes = [
    { id: 'pachinko', title: 'ゴールド', sub: 'はじまりに、少しの光。', icon: 'flare' },
    { id: 'mahjong', title: 'ステップ', sub: '一手ずつ、着実に。', icon: 'task_alt' },
    { id: 'simple', title: 'シンプル', sub: '静かに、必要な情報を。', icon: 'notifications' },
  ] as const satisfies readonly { id: NoticeMode; title: string; sub: string; icon: string }[];
  protected readonly policyModes = (Object.keys(timePolicyModeLabels) as TimePolicyMode[]).map(
    (id) => ({ id, label: timePolicyModeLabels[id] }),
  );
  protected readonly limits: readonly { value: DailyLimit; label: string }[] = [
    { value: 600, label: '10分' },
    { value: 1200, label: '20分' },
    { value: 1800, label: '30分' },
  ];
  protected readonly syncStatusLabels = syncStatusLabels;
  protected readonly browserTimeZone = Intl.DateTimeFormat().resolvedOptions().timeZone;

  protected readonly mainForm = new FormGroup({
    mode: new FormControl<NoticeMode>('pachinko', { nonNullable: true }),
    snoozeMinutes: new FormControl(10, {
      nonNullable: true,
      validators: [required, Validators.min(1), Validators.max(120)],
    }),
    sound: new FormControl(true, { nonNullable: true }),
    reducedMotion: new FormControl(false, { nonNullable: true }),
    grime: new FormControl(false, { nonNullable: true }),
    name: new FormControl('', { nonNullable: true, validators: [Validators.maxLength(100)] }),
    context: new FormControl('', { nonNullable: true, validators: [Validators.maxLength(2000)] }),
  });
  protected readonly policyForm = new FormGroup({
    mode: new FormControl<TimePolicyMode>('rewards', { nonNullable: true }),
    dailyLimitSeconds: new FormControl<DailyLimit>(1800, { nonNullable: true }),
  });
  protected readonly sleepForm = new FormGroup({
    bedtime: new FormControl('23:30', { nonNullable: true, validators: [required] }),
    wake: new FormControl('07:00', { nonNullable: true, validators: [required] }),
  });

  protected readonly main = signal<MainValue>(this.mainForm.getRawValue());
  protected readonly mainValid = signal(true);
  protected readonly policy = signal<PolicyValue>(this.policyForm.getRawValue());

  /** 「変更を保存」を実行している間（設定と作業の傾向を順に保存する）。 */
  protected readonly saving = signal(false);
  /** 作業の傾向の保存に失敗した（設定は保存できたときも）。 */
  protected readonly profileFailed = signal(false);

  private syncedSettings: Settings | undefined;
  private syncedProfile: Profile | undefined;
  private syncedPolicy: PolicyValue | undefined;
  private syncedSleep: SleepValue | undefined;

  protected readonly settingsChanged = computed(() => {
    const saved = this.settings.value();
    return saved !== undefined && !sameKeys(this.main(), saved, settingKeys);
  });
  protected readonly profileChanged = computed(() => {
    const saved = this.profile.value();
    return saved !== undefined && !sameKeys(this.main(), saved, profileKeys);
  });
  protected readonly changed = computed(() => this.settingsChanged() || this.profileChanged());

  protected readonly selectedMode = computed(
    () => this.modes.find((mode) => mode.id === this.main().mode) ?? this.modes[0],
  );
  protected readonly policyTitle = computed(() => {
    const mode = this.wallet.value()?.policy.mode;
    return mode ? timePolicyModeLabels[mode] : '';
  });
  protected readonly unlimitedPolicy = computed(
    () => this.policy().mode === 'reminder' || this.policy().mode === 'self',
  );
  /** ルールの説明。数値はサーバーのルールの定数から作る。 */
  protected readonly policyHelp = computed(() => {
    const rules = this.wallet.value()?.rules;
    switch (this.policy().mode) {
      case 'rewards':
        return rules
          ? `作業${durationLabel(rules.workSecondsPerReward)}で視聴${durationLabel(rules.rewardSecondsPerUnit)}（1日${durationLabel(rules.dailyRewardCapSeconds)}まで）。毎日の補充もあります。`
          : '作業に応じて視聴時間が増えます。毎日の補充もあります。';
      case 'fixed':
        return '毎日、選んだ枠まで補充。作業での追加はなし。';
      case 'reminder': {
        const choices = rules?.viewingChoicesSeconds.map((seconds) => durationLabel(seconds));
        return `上限なし。${choices?.join('・') ?? '5分・10分'}の区切りだけ知らせます。`;
      }
      case 'self':
        return '視聴枠と区切りの表示を止めます。';
    }
  });
  protected readonly zoneMismatch = computed(() => {
    const zone = this.profile.value()?.timeZone;
    return zone !== undefined && zone !== this.browserTimeZone;
  });
  protected readonly calendarEnabled = computed(
    () =>
      this.config.config()?.features.calendar === true && this.calendar.value()?.enabled !== false,
  );

  constructor() {
    this.shortcut.ensureLoaded();
    this.sleep.ensureLoaded();
    const destroyRef = inject(DestroyRef);
    // 利用者が編集したときだけ、前回の保存の失敗表示を消す。
    // store の値に合わせる patch は emitEvent: false で行うので、ここには来ない。
    this.mainForm.valueChanges.pipe(takeUntilDestroyed(destroyRef)).subscribe(() => {
      this.refreshMain();
      this.profileFailed.set(false);
    });
    this.policyForm.valueChanges.pipe(takeUntilDestroyed(destroyRef)).subscribe(() => {
      this.policy.set(this.policyForm.getRawValue());
    });
    destroyRef.onDestroy(() => {
      this.shortcut.forgetIssuedKey();
    });

    // 声かけだけ・自分で区切るときは、1日の上限を使わない。
    effect(() => {
      const control = this.policyForm.controls.dailyLimitSeconds;
      if (this.unlimitedPolicy()) control.disable({ emitEvent: false });
      else control.enable({ emitEvent: false });
    });
    // 保存していない編集が無いまとまりだけ、store の最新の値に合わせる。
    effect(() => {
      const saved = this.settings.value();
      if (saved)
        untracked(() => {
          this.syncSettings(saved);
        });
    });
    effect(() => {
      const saved = this.profile.value();
      if (saved)
        untracked(() => {
          this.syncProfile(saved);
        });
    });
    effect(() => {
      const policy = this.wallet.value()?.policy;
      if (policy) {
        untracked(() => {
          this.syncPolicy({
            mode: policy.mode,
            dailyLimitSeconds: toDailyLimit(policy.dailyLimitSeconds),
          });
        });
      }
    });
    effect(() => {
      const sleep = this.sleep.sleep();
      if (sleep)
        untracked(() => {
          this.syncSleep({ bedtime: sleep.bedtime, wake: sleep.wake });
        });
    });
  }

  private refreshMain(): void {
    this.main.set(this.mainForm.getRawValue());
    this.mainValid.set(this.mainForm.valid);
  }

  private syncSettings(saved: Settings): void {
    const current = this.mainForm.getRawValue();
    if (!this.syncedSettings || sameKeys(current, this.syncedSettings, settingKeys)) {
      this.mainForm.patchValue(
        {
          mode: saved.mode,
          snoozeMinutes: saved.snoozeMinutes,
          sound: saved.sound,
          reducedMotion: saved.reducedMotion,
          grime: saved.grime,
        },
        { emitEvent: false },
      );
      this.refreshMain();
    }
    this.syncedSettings = saved;
  }

  private syncProfile(saved: Profile): void {
    const current = this.mainForm.getRawValue();
    if (!this.syncedProfile || sameKeys(current, this.syncedProfile, profileKeys)) {
      this.mainForm.patchValue({ name: saved.name, context: saved.context }, { emitEvent: false });
      this.refreshMain();
    }
    this.syncedProfile = saved;
  }

  private syncPolicy(saved: PolicyValue): void {
    const current = this.policyForm.getRawValue();
    if (!this.syncedPolicy || sameKeys(current, this.syncedPolicy, ['mode', 'dailyLimitSeconds'])) {
      this.policyForm.setValue(saved);
    }
    this.syncedPolicy = saved;
  }

  private syncSleep(saved: SleepValue): void {
    const current = this.sleepForm.getRawValue();
    if (!this.syncedSleep || sameKeys(current, this.syncedSleep, ['bedtime', 'wake'])) {
      this.sleepForm.setValue(saved);
    }
    this.syncedSleep = saved;
  }

  /** 変更したまとまりだけを保存する。失敗したまとまりは、次に押したときに保存し直す。 */
  protected async save(): Promise<void> {
    if (this.saving() || !this.mainForm.valid || !this.changed()) return;
    this.saving.set(true);
    this.profileFailed.set(false);
    try {
      const value = this.mainForm.getRawValue();
      if (this.settingsChanged()) {
        const outcome = await this.settings.update({
          mode: value.mode,
          snoozeMinutes: value.snoozeMinutes,
          sound: value.sound,
          reducedMotion: value.reducedMotion,
          grime: value.grime,
        });
        if (!outcome.ok) return;
      }
      const profile = this.profile.value();
      if (this.profileChanged() && profile) {
        const outcome = await this.profile.update({
          name: value.name,
          context: value.context,
          timeZone: profile.timeZone,
        });
        if (!outcome.ok) {
          this.profileFailed.set(true);
          return;
        }
      }
      this.snackBar.open('設定を保存しました', '閉じる', { duration: 3000 });
    } finally {
      this.saving.set(false);
    }
  }

  protected async useBrowserTimeZone(): Promise<void> {
    const profile = this.profile.value();
    if (!profile) return;
    const outcome = await this.profile.update({
      name: profile.name,
      context: profile.context,
      timeZone: this.browserTimeZone,
    });
    if (outcome.ok) this.snackBar.open('タイムゾーンを保存しました', '閉じる', { duration: 3000 });
  }

  protected async savePolicy(): Promise<void> {
    if (this.work.session()) return;
    const value = this.policyForm.getRawValue();
    const outcome = await this.wallet.updatePolicy(value);
    if (outcome.ok) {
      this.snackBar.open('視聴時間の設定を保存しました', '閉じる', { duration: 3000 });
    }
  }

  protected async saveSleep(): Promise<void> {
    if (this.sleepForm.invalid) return;
    const outcome = await this.sleep.update(this.sleepForm.getRawValue());
    if (!outcome.ok) return;
    this.snackBar.open(
      outcome.value.sleep?.calendarPending
        ? '就寝の予定を保存しました（カレンダーへの登録は再試行してください）'
        : '就寝の予定を保存しました',
      '閉じる',
      { duration: 4000 },
    );
  }

  protected async removeSleep(): Promise<void> {
    const outcome = await this.sleep.remove();
    if (outcome.ok) this.snackBar.open('就寝の予定を外しました', '閉じる', { duration: 4000 });
  }

  /** 確認してから連携を解除する。解除後は、連携状態と課題（カレンダーとの結び付き）を取り直す。 */
  protected async disconnectCalendar(): Promise<void> {
    const ref = this.dialog.open<DisconnectCalendarDialog, undefined, boolean>(
      DisconnectCalendarDialog,
      { width: '28rem', maxWidth: 'calc(100vw - 2rem)', autoFocus: 'first-heading' },
    );
    if ((await firstValueFrom(ref.afterClosed())) !== true) return;
    void this.calendar.refresh();
    void this.tasks.refresh();
    this.snackBar.open('Googleカレンダーとの連携を解除しました', '閉じる', { duration: 4000 });
  }

  protected async issueShortcut(): Promise<void> {
    await this.shortcut.issueKey();
  }

  protected async revokeShortcut(): Promise<void> {
    const outcome = await this.shortcut.revokeKey();
    if (outcome.ok) this.snackBar.open('接続キーを無効にしました', '閉じる', { duration: 3000 });
  }

  protected copyShortcutKey(key: string): void {
    const copied = this.clipboard.copy(key);
    this.snackBar.open(
      copied
        ? '接続キーをコピーしました'
        : 'コピーできませんでした。キーを選択してコピーしてください。',
      '閉じる',
      { duration: 3000 },
    );
  }
}
