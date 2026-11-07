import { Component, inject } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatSlideToggleModule } from '@angular/material/slide-toggle';
import { MatSelectModule } from '@angular/material/select';
import { MatSnackBar } from '@angular/material/snack-bar';
import { Api } from './api';
import { TaskActions } from './task-actions';

@Component({
  selector: 'app-settings',
  imports: [
    FormsModule,
    MatButtonModule,
    MatFormFieldModule,
    MatInputModule,
    MatSlideToggleModule,
    MatSelectModule,
  ],
  templateUrl: './settings.html',
})
export class Settings {
  readonly api = inject(Api);
  readonly actions = inject(TaskActions);
  private readonly snack = inject(MatSnackBar);
  readonly settings = { ...this.api.state()!.settings };
  readonly profile = { ...this.api.state()!.profile };
  readonly timePolicy = {
    mode: this.api.state()!.timePolicy?.mode ?? 'rewards',
    dailyLimitSeconds: this.api.state()!.timePolicy?.dailyLimitSeconds ?? 1800,
  };
  async saveTimePolicy() {
    if (await this.api.mutate('/time-policy', this.timePolicy, 'PUT'))
      this.snack.open('視聴時間の設定を保存しました', '閉じる', { duration: 3000 });
  }
  async saveSettings() {
    if (await this.api.mutate('/settings', this.settings, 'PUT'))
      this.snack.open('設定を保存しました', '閉じる', { duration: 3000 });
  }
  async saveProfile() {
    if (await this.api.mutate('/profile', this.profile, 'PUT'))
      this.snack.open('作業の傾向を保存しました', '閉じる', { duration: 3000 });
  }
}
