import { ChangeDetectionStrategy, Component, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCheckboxModule } from '@angular/material/checkbox';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { newIdempotencyKey } from '@dopagaki/contract';
import { AuthService } from '../../core/auth/auth.service';
import { AccountScope } from '../../core/auth/account-scope';
import { API_CLIENT, idempotent, requireData, requireOk } from '../../core/http/api-client';
import { AccountSwitchedError, Operation } from '../../core/http/operation';
import { ProfileStore } from './profile.store';
import { AssistantStore } from '../assistant/assistant.store';
function download(name: string, type: string, text: string): void {
  const url = URL.createObjectURL(new Blob([text], { type }));
  const a = document.createElement('a');
  a.href = url;
  a.download = name;
  a.click();
  setTimeout(() => {
    URL.revokeObjectURL(url);
  }, 1000);
}
export function csvCell(value: string): string {
  return (
    '"' +
    (/^[=+\-@\t\r\n]/.test(value.trimStart()) ? "'" + value : value).replaceAll('"', '""') +
    '"'
  );
}
@Component({
  selector: 'app-account-tools',
  imports: [FormsModule, MatButtonModule, MatCheckboxModule, MatFormFieldModule, MatInputModule],
  template: `
    <h3>データと利用停止</h3>
    <p>課題・記録・会話を書き出せます。認証情報は含めません。</p>
    <button
      mat-stroked-button
      type="button"
      (click)="exportData(false)"
      [disabled]="exporting.pending()"
    >
      JSONで書き出す
    </button>
    <button mat-button type="button" (click)="exportData(true)" [disabled]="exporting.pending()">
      CSVで書き出す
    </button>
    @if (exporting.error(); as error) {
      <p role="alert">{{ error.message }}</p>
    }
    <h4>会話を削除</h4>
    <mat-checkbox [(ngModel)]="forgetMemory" [ngModelOptions]="{ standalone: true }">
      保存した作業の傾向も削除する
    </mat-checkbox>
    <p>会話の履歴をすべて削除します。元に戻せません。</p>
    <mat-checkbox [(ngModel)]="confirmHistory" [ngModelOptions]="{ standalone: true }">
      会話を削除することを確認しました
    </mat-checkbox>
    <button
      mat-button
      type="button"
      (click)="deleteHistory()"
      [disabled]="history.pending() || !confirmHistory"
    >
      会話を削除する
    </button>
    @if (history.error(); as error) {
      <p role="alert">{{ error.message }}</p>
    }
    <p role="status">{{ historyDeleted() ? '会話を削除しました。' : '' }}</p>
    <h4>退会</h4>
    <p>
      予定の連携を停止し、課題・記録・会話・アカウントを削除します。Googleにある専用カレンダーは残ります。元に戻せません。
    </p>
    <mat-form-field>
      <mat-label>確認のため DELETE と入力</mat-label>
      <input
        matInput
        [(ngModel)]="confirmation"
        [ngModelOptions]="{ standalone: true }"
        autocomplete="off"
      />
    </mat-form-field>
    <button
      mat-stroked-button
      type="button"
      (click)="deleteAccount()"
      [disabled]="deleting.pending() || confirmation !== 'DELETE'"
    >
      再認証して退会する
    </button>
    @if (deleting.error(); as error) {
      <p role="alert">{{ error.message }}</p>
    }
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class AccountTools {
  private readonly api = inject(API_CLIENT);
  private readonly auth = inject(AuthService);
  private readonly profile = inject(ProfileStore);
  private readonly assistant = inject(AssistantStore);
  private readonly scope = inject(AccountScope);
  protected readonly exporting = new Operation(this.scope);
  protected readonly history = new Operation(this.scope);
  protected readonly deleting = new Operation(this.scope);
  protected readonly historyDeleted = signal(false);
  protected forgetMemory = false;
  protected confirmHistory = false;
  protected confirmation = '';
  private readonly deletionKey = newIdempotencyKey();
  async exportData(csv: boolean): Promise<void> {
    await this.exporting.run(
      () => requireData(this.api.GET('/account/export')),
      (data) => {
        if (csv) {
          const rows = [['collection', 'id', 'data'].map(csvCell).join(',')];
          for (const [collection, items] of Object.entries(data))
            for (const row of items)
              rows.push(
                [collection, typeof row['_id'] === 'string' ? row['_id'] : '', JSON.stringify(row)]
                  .map(csvCell)
                  .join(','),
              );
          download('dopagaki.csv', 'text/csv;charset=utf-8', '\ufeff' + rows.join('\r\n'));
        } else download('dopagaki.json', 'application/json', JSON.stringify(data, null, 2));
      },
    );
  }
  async deleteHistory(): Promise<void> {
    if (!this.confirmHistory) return;
    this.historyDeleted.set(false);
    const result = await this.history.run(() =>
      requireOk(
        this.api.DELETE('/assistant/turns', {
          params: { header: idempotent(), query: { forgetMemory: this.forgetMemory } },
        }),
      ),
    );
    if (result.ok) {
      this.historyDeleted.set(true);
      await Promise.all([this.profile.refresh(), this.assistant.refresh()]);
    }
  }
  async deleteAccount(): Promise<void> {
    if (this.confirmation !== 'DELETE') return;
    const epoch = this.scope.epoch();
    const result = await this.deleting.run(async () => {
      if (this.auth.mode() === 'live') await this.auth.reauthenticate();
      if (epoch !== this.scope.epoch()) throw new AccountSwitchedError();
      return requireData(
        this.api.POST('/account/deletion', {
          params: { header: idempotent(this.deletionKey) },
          body: { confirmation: 'DELETE' },
        }),
      );
    });
    if (result.ok) {
      const url = '/deletion-status#' + result.value.receipt;
      await this.auth.logout();
      location.assign(url);
    }
  }
}
