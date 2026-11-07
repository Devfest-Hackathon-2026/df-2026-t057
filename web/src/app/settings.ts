import { Component, inject } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatSlideToggleModule } from '@angular/material/slide-toggle';
import { MatSnackBar } from '@angular/material/snack-bar';
import { Api } from './api';
import { TaskActions } from './task-actions';

@Component({
  selector: 'app-settings',
  imports: [FormsModule, MatButtonModule, MatFormFieldModule, MatInputModule, MatSlideToggleModule],
  templateUrl: './settings.html',
})
export class Settings {
  readonly api = inject(Api);
  readonly actions = inject(TaskActions);
  private readonly snack = inject(MatSnackBar);
  readonly settings = { ...this.api.state()!.settings };
  readonly profile = { ...this.api.state()!.profile };
  readonly sleep = {
    bedtime: this.api.state()!.sleep?.bedtime ?? '23:30',
    wake: this.api.state()!.sleep?.wake ?? '07:00',
  };
  async saveSettings() {
    if (await this.api.mutate('/settings', this.settings, 'PUT'))
      this.snack.open('設定を保存しました', '閉じる', { duration: 3000 });
  }
  async saveProfile() {
    if (await this.api.mutate('/profile', this.profile, 'PUT'))
      this.snack.open('作業の傾向を保存しました', '閉じる', { duration: 3000 });
  }
  async saveSleep(clear = false) {
    const body = clear ? { bedtime: '', wake: '' } : this.sleep;
    if (await this.api.mutate('/sleep', body, 'PUT'))
      this.snack.open(clear ? '就寝の予定を外しました' : '就寝の予定を保存しました', '閉じる', {
        duration: 3000,
      });
  }
}
