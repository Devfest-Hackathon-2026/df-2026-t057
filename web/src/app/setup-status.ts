import { Component, inject, OnDestroy, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { MatButtonModule } from '@angular/material/button';
import { Api } from './api';
import { TaskActions } from './task-actions';

interface Diagnostics {
  accountId?: string;
  email: string;
  notificationPermission: string;
  protocolVersion: number;
}
@Component({
  selector: 'app-setup-status',
  imports: [MatButtonModule, RouterLink],
  template: `
    <section aria-label="使い始めるための接続確認">
      <h2>接続を確認</h2>
      <ol>
        <li>Googleログイン：{{ api.signedIn() ? 'ログイン済み' : '未ログイン' }}</li>
        <li>
          Googleカレンダー：{{ api.state()?.calendarConnected ? '連携済み' : '未連携' }}
          <button
            mat-button
            type="button"
            (click)="actions.connect()"
            [disabled]="!api.config()?.calendarEnabled"
          >
            連携する
          </button>
        </li>
        <li>
          Chrome拡張：{{ diagnostics() ? '応答あり' : '未確認' }}
          <button mat-button type="button" (click)="check()" [disabled]="checking()">
            {{ checking() ? '確認中…' : '確認する' }}
          </button>
        </li>
        <li>
          拡張のアカウント：{{ diagnostics()?.email || '拡張メニューからGoogleで接続してください' }}
          @if (
            diagnostics() &&
            diagnostics()?.email !== api.accountEmail() &&
            api.config()?.mode === 'live'
          ) {
            <p role="alert">管理画面と拡張のアカウントが異なります。</p>
          }
        </li>
        <li>Chrome通知：{{ diagnostics()?.notificationPermission || '未確認' }}</li>
        <li>
          通知の見え方：拡張メニューの「テスト通知を送る」で確認してください。
          <button mat-button type="button" (click)="seen.set(true)">
            {{ seen() ? '見えたことを確認済み' : '通知が見えた' }}
          </button>
        </li>
      </ol>
      <p role="status">{{ note() }}</p>
      <p>
        Chromeを終了している間やスリープ中は通知できません。拡張の導入・更新は
        <a href="/extension-help.html" target="_blank" rel="noopener">導入手順</a>
        を参照してください。
      </p>
      <a mat-button routerLink="/calendar">予定に戻る</a>
    </section>
  `,
})
export class SetupStatus implements OnDestroy {
  readonly api = inject(Api);
  readonly actions = inject(TaskActions);
  readonly diagnostics = signal<Diagnostics | null>(null);
  readonly checking = signal(false);
  readonly note = signal('');
  readonly seen = signal(false);
  private requestId = '';
  private timer?: ReturnType<typeof setTimeout>;
  private readonly receive = (event: MessageEvent) => {
    if (
      event.source !== window ||
      event.origin !== location.origin ||
      event.data?.type !== 'DOPAGAKI_DIAGNOSTICS' ||
      event.data.requestId !== this.requestId
    )
      return;
    clearTimeout(this.timer);
    this.checking.set(false);
    if (event.data.diagnostics?.protocolVersion === 2) {
      this.diagnostics.set(event.data.diagnostics);
      this.note.set('拡張から応答がありました。');
    } else this.note.set('拡張を最新版へ更新して、もう一度確認してください。');
  };
  constructor() {
    window.addEventListener('message', this.receive);
  }
  check() {
    this.requestId = crypto.randomUUID();
    this.checking.set(true);
    this.note.set('');
    window.postMessage({ type: 'DOPAGAKI_DIAGNOSE', requestId: this.requestId }, location.origin);
    clearTimeout(this.timer);
    this.timer = setTimeout(() => {
      this.checking.set(false);
      this.note.set('拡張の応答がありません。導入・更新後にこの画面を開き直してください。');
    }, 5000);
  }
  ngOnDestroy() {
    window.removeEventListener('message', this.receive);
    clearTimeout(this.timer);
  }
}
