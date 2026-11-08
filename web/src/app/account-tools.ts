import { Component, inject, signal, output } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatExpansionModule } from '@angular/material/expansion';
import { MatCheckboxModule } from '@angular/material/checkbox';
import { MatDialog, MAT_DIALOG_DATA, MatDialogModule } from '@angular/material/dialog';
import { Api, message } from './api';
import { SetupStatus } from './setup-status';
@Component({
  imports: [MatDialogModule, MatButtonModule],
  template: `
    <h2 mat-dialog-title>操作の確認</h2>
    <mat-dialog-content>{{ text }}</mat-dialog-content>
    <mat-dialog-actions>
      <button mat-button [mat-dialog-close]="false">キャンセル</button>
      <button mat-flat-button [mat-dialog-close]="true">実行する</button>
    </mat-dialog-actions>
  `,
})
class ConfirmAccountOperation {
  readonly text = inject<string>(MAT_DIALOG_DATA);
}
@Component({
  selector: 'app-account-tools',
  imports: [FormsModule, MatButtonModule, MatCheckboxModule, MatExpansionModule, SetupStatus],
  template: `
    <mat-accordion>
      <mat-expansion-panel>
        <mat-expansion-panel-header>
          <mat-panel-title>導入と接続確認</mat-panel-title>
        </mat-expansion-panel-header>
        <app-setup-status />
      </mat-expansion-panel>
      <mat-expansion-panel>
        <mat-expansion-panel-header>
          <mat-panel-title>データとアカウント</mat-panel-title>
        </mat-expansion-panel-header>
        <section aria-label="データとアカウント">
          <h2>データとアカウント</h2>
          <button
            mat-stroked-button
            type="button"
            (click)="download('json')"
            [disabled]="!!pending()"
          >
            データを書き出す（JSON）
          </button>
          <button
            mat-stroked-button
            type="button"
            (click)="download('csv')"
            [disabled]="!!pending()"
          >
            課題を書き出す（CSV）
          </button>
          <p>連携解除ではアプリ内の課題と記録を残し、Google側のアクセス許可は取り消しません。</p>
          <button
            mat-button
            type="button"
            (click)="operate('disconnect')"
            [disabled]="
              !!pending() ||
              (!api.state()?.calendarConnected && api.state()?.calendarSync !== 'disconnecting')
            "
          >
            Googleカレンダー連携を解除
          </button>
          <p>
            <mat-checkbox [(ngModel)]="deleteMemory" [ngModelOptions]="{ standalone: true }">
              会話の削除と一緒に作業の傾向も削除する
            </mat-checkbox>
          </p>
          <button mat-button type="button" (click)="operate('history')" [disabled]="!!pending()">
            AI会話をすべて削除
          </button>
          <p>
            <mat-checkbox [(ngModel)]="deleteCalendar" [ngModelOptions]="{ standalone: true }">
              退会時に専用DOPAGAKIカレンダーも削除する
            </mat-checkbox>
          </p>
          <button
            mat-button
            type="button"
            (click)="operate('account')"
            [disabled]="!!pending() || !api.config()?.accountDeletionEnabled"
          >
            退会してデータを削除
          </button>
          <p role="status">{{ pending() ? '処理しています…' : notice() }}</p>
          @if (error()) {
            <p role="alert">{{ error() }}</p>
          }
        </section>
      </mat-expansion-panel>
    </mat-accordion>
  `,
})
export class AccountTools {
  readonly memoryDeleted = output<void>();
  readonly api = inject(Api);
  private dialog = inject(MatDialog);
  readonly pending = signal('');
  readonly notice = signal('');
  readonly error = signal('');
  deleteMemory = false;
  deleteCalendar = false;
  async download(format: 'json' | 'csv') {
    this.pending.set('export');
    this.error.set('');
    try {
      await this.api.download(format);
      this.notice.set('書き出しが完了しました。');
    } catch (e) {
      this.error.set(message(e));
    } finally {
      this.pending.set('');
    }
  }
  operate(kind: 'disconnect' | 'history' | 'account') {
    const text = {
      disconnect: 'Googleカレンダーとの連携を停止します。課題と記録は残ります。',
      history: '保存済みのAI会話をすべて削除します。この操作は取り消せません。',
      account:
        '課題・記録・会話・保存した連携トークンを削除し、退会します。' +
        (this.deleteCalendar
          ? '専用DOPAGAKIカレンダーも削除します。'
          : 'Googleの専用カレンダーは残します。'),
    };
    this.dialog
      .open(ConfirmAccountOperation, { data: text[kind] })
      .afterClosed()
      .subscribe((confirmed) => {
        if (confirmed) void this.run(kind);
      });
  }
  private async run(kind: 'disconnect' | 'history' | 'account') {
    if (this.pending()) return;
    this.pending.set(kind);
    this.error.set('');
    try {
      if (kind === 'account') {
        await this.api.reauthenticate();
        const result = await this.api.request<{ receipt?: string }>('/account/deletion', 'POST', {
          deleteCalendar: this.deleteCalendar,
        });
        if (result.receipt) sessionStorage.setItem('dopagaki-deletion-receipt', result.receipt);
        await this.api.logout();
        location.assign(result.receipt ? '/deletion-status.html' : '/?deleted=true');
      } else if (kind === 'history') {
        await this.api.request('/assistant/delete-history', 'POST', {
          deleteMemory: this.deleteMemory,
        });
        await this.api.refresh();
        if (this.deleteMemory) this.memoryDeleted.emit();
      } else {
        await this.api.request('/calendar/disconnect', 'POST', {});
        await this.api.refresh();
      }
      this.notice.set(
        kind === 'account'
          ? '削除を受け付けました。処理が完了するまで再登録をお控えください。'
          : '操作が完了しました。',
      );
    } catch (e) {
      this.error.set(message(e));
      if (kind === 'history' && this.deleteMemory) {
        await this.api.load();
        if (this.api.state()?.profile.context === '') this.memoryDeleted.emit();
      }
    } finally {
      this.pending.set('');
    }
  }
}
