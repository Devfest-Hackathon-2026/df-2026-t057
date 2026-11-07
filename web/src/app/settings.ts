import { Component, inject, signal } from '@angular/core';
import { DatePipe } from '@angular/common';
import { FormsModule, NgForm } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatSelectModule } from '@angular/material/select';
import { MatExpansionModule } from '@angular/material/expansion';
import { MatSlideToggleModule } from '@angular/material/slide-toggle';
import { MatSnackBar } from '@angular/material/snack-bar';
import { Api } from './api';
import { TaskActions } from './task-actions';

@Component({
  selector: 'app-settings',
  imports: [
    DatePipe,
    FormsModule,
    MatButtonModule,
    MatFormFieldModule,
    MatInputModule,
    MatSlideToggleModule,
    MatExpansionModule,
    MatSelectModule,
  ],
  templateUrl: './settings.html',
  styleUrl: './settings.scss',
})
export class Settings {
  readonly api = inject(Api);
  readonly actions = inject(TaskActions);
  private readonly snack = inject(MatSnackBar);
  readonly settings = { ...this.api.state()!.settings };
  readonly profile = { ...this.api.state()!.profile };
  readonly saving = signal(false);
  readonly timePolicy = {
    mode: this.api.state()!.timePolicy?.mode ?? 'rewards',
    dailyLimitSeconds: this.api.state()!.timePolicy?.dailyLimitSeconds ?? 1800,
  };
  timePolicyTitle() {
    return {
      rewards: '作業に応じて増やす',
      fixed: '固定枠',
      reminder: '区切りの通知だけ',
      self: '自分で管理する',
    }[this.timePolicy.mode];
  }
  async saveTimePolicy() {
    if (this.saving() || this.api.busy() || this.api.state()?.session) return;
    if (await this.api.mutate('/time-policy', this.timePolicy, 'PUT'))
      this.snack.open('視聴時間の設定を保存しました', '閉じる', { duration: 3000 });
  }
  // The key is shown once after issuing; the server keeps only its hash.
  readonly shortcutKey = signal('');
  readonly shortcutURL = `${location.origin}/api/shortcut/open`;
  async issueShortcut() {
    const issued = await this.api
      .request<{ key: string }>('/shortcut/key', 'POST')
      .catch((e: Error) => (this.api.error.set(e.message), null));
    if (!issued) return;
    this.shortcutKey.set(issued.key);
    await this.api.refresh();
  }
  async revokeShortcut() {
    if (await this.api.mutate('/shortcut/key', undefined, 'DELETE')) {
      this.shortcutKey.set('');
      this.snack.open('接続キーを無効にしました', '閉じる', { duration: 3000 });
    }
  }
  async copyShortcutKey() {
    await navigator.clipboard.writeText(this.shortcutKey());
    this.snack.open('接続キーをコピーしました', '閉じる', { duration: 3000 });
  }
  readonly sleep = {
    bedtime: this.api.state()!.sleep?.bedtime ?? '23:30',
    wake: this.api.state()!.sleep?.wake ?? '07:00',
  };

  readonly modes = [
    { id: 'pachinko', title: 'ゴールド', sub: 'はじまりに、少しの光。', icon: 'flare' },
    { id: 'mahjong', title: 'ステップ', sub: '一手ずつ、着実に。', icon: 'task_alt' },
    { id: 'simple', title: 'シンプル', sub: '静かに、必要な情報を。', icon: 'notifications' },
  ] as const;
  selectedMode() {
    return this.modes.find((mode) => mode.id === this.settings.mode)!;
  }
  settingsChanged() {
    const saved = this.api.state()!.settings;
    return Object.entries(this.settings).some(
      ([key, value]) => saved[key as keyof typeof saved] !== value,
    );
  }
  profileChanged() {
    const saved = this.api.state()!.profile;
    return this.profile.name !== saved.name || this.profile.context !== saved.context;
  }
  changed() {
    return this.settingsChanged() || this.profileChanged();
  }
  async save(form: NgForm) {
    if (this.api.busy() || this.saving() || form.invalid || !this.changed()) return;
    this.saving.set(true);
    try {
      if (this.settingsChanged() && !(await this.api.mutate('/settings', this.settings, 'PUT')))
        return;
      if (this.profileChanged() && !(await this.api.mutate('/profile', this.profile, 'PUT')))
        return;
      this.snack.open('設定を保存しました', '閉じる', { duration: 3000 });
    } finally {
      this.saving.set(false);
    }
  }
  async saveSleep(clear = false) {
    if (this.saving() || this.api.busy()) return;
    const body = clear ? { bedtime: '', wake: '' } : this.sleep;
    if (await this.api.mutate('/sleep', body, 'PUT'))
      this.snack.open(
        clear
          ? '就寝の予定を外しました'
          : this.api.state()?.sleep?.calendarPending
            ? '就寝の予定を保存しました（カレンダーへの登録は再試行してください）'
            : '就寝の予定を保存しました',
        '閉じる',
        { duration: 4000 },
      );
  }
}
