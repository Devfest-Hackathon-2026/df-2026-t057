import { Component, computed, effect, inject, output, signal, viewChild } from '@angular/core';
import { NgTemplateOutlet } from '@angular/common';
import { MatButtonModule } from '@angular/material/button';
import { MatMenuModule, MatMenuTrigger } from '@angular/material/menu';
import { MatProgressSpinnerModule } from '@angular/material/progress-spinner';
import { Api } from './api';
import { TutorialService } from './tutorial';

@Component({
  selector: 'app-account-menu',
  imports: [NgTemplateOutlet, MatButtonModule, MatMenuModule, MatProgressSpinnerModule],
  templateUrl: './account-menu.html',
  styleUrl: './account-menu.scss',
})
export class AccountMenu {
  readonly api = inject(Api);
  readonly tutorial = inject(TutorialService);
  readonly openSettings = output<void>();
  readonly trigger = viewChild(MatMenuTrigger);
  readonly photoError = signal('');
  readonly name = computed(() => this.api.accountName() || this.api.accountEmail());
  readonly initial = computed(() => this.name().slice(0, 1).toUpperCase());

  constructor() {
    effect(() => {
      if (this.api.signedIn() && !this.api.authPending() && !this.api.loading())
        this.trigger()?.closeMenu();
    });
  }
}
