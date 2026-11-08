import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatDividerModule } from '@angular/material/divider';
import { MatIconModule } from '@angular/material/icon';
import { MatMenuModule } from '@angular/material/menu';
import { RouterLink } from '@angular/router';
import { NgTemplateOutlet } from '@angular/common';
import { AuthService } from '../../core/auth/auth.service';
import { LocaleStore } from '../../core/i18n/locale.store';
import { CalendarStore } from '../calendar/calendar.store';
import { Tutorial } from '../onboarding/tutorial.service';

/** 右上のアカウントメニュー（live）。Chrome のアカウントメニューに似せる。 */
@Component({
  selector: 'app-account-menu',
  imports: [
    NgTemplateOutlet,
    MatButtonModule,
    MatDividerModule,
    MatIconModule,
    MatMenuModule,
    RouterLink,
  ],
  templateUrl: './account-menu.html',
  styleUrl: './account-menu.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class AccountMenu {
  protected readonly auth = inject(AuthService);
  protected readonly calendar = inject(CalendarStore);
  protected readonly tutorial = inject(Tutorial);
  protected readonly text = inject(LocaleStore).web;
  /** 読み込めなかった写真の URL。 */
  protected readonly photoError = signal('');

  protected readonly email = computed(() => this.auth.account()?.email ?? '');
  protected readonly name = computed(() => {
    const account = this.auth.account();
    const name = account?.name ?? '';
    return name !== '' ? name : (account?.email ?? '');
  });
  protected readonly initial = computed(() => this.name().slice(0, 1).toUpperCase());
  protected readonly photo = computed(() => {
    const url = this.auth.account()?.photoUrl ?? '';
    return url && url !== this.photoError() ? url : '';
  });
  protected readonly calendarLabel = computed(() => {
    const text = this.text().shell.accountMenu;
    if (!this.calendar.loaded()) return text.calendarChecking;
    return this.calendar.connected() ? text.calendarConnected : text.calendarConnect;
  });
}
