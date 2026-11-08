import {
  inject,
  provideAppInitializer,
  provideBrowserGlobalErrorListeners,
  type ApplicationConfig,
} from '@angular/core';
import { provideNativeDateAdapter } from '@angular/material/core';
import { MatIconRegistry } from '@angular/material/icon';
import { MAT_SNACK_BAR_DEFAULT_OPTIONS } from '@angular/material/snack-bar';
import { MAT_TOOLTIP_DEFAULT_OPTIONS } from '@angular/material/tooltip';
import { TitleStrategy, provideRouter, withComponentInputBinding } from '@angular/router';
import { routes } from './app.routes';
import { LocaleStore } from './core/i18n/locale.store';
import { LocalizedTitleStrategy } from './core/i18n/localized-title-strategy';

export const appConfig: ApplicationConfig = {
  providers: [
    provideBrowserGlobalErrorListeners(),
    provideRouter(routes, withComponentInputBinding()),
    { provide: TitleStrategy, useClass: LocalizedTitleStrategy },
    provideNativeDateAdapter(),
    { provide: MAT_SNACK_BAR_DEFAULT_OPTIONS, useValue: { duration: 4000 } },
    // ツールチップは補足。キーボードで移動したときにすぐ消えないよう、少し長めに出す。
    {
      provide: MAT_TOOLTIP_DEFAULT_OPTIONS,
      useValue: { showDelay: 300, hideDelay: 200, touchendHideDelay: 1500 },
    },
    provideAppInitializer(() => {
      // public/material-symbols-rounded.woff2（自己ホスト）を mat-icon の既定にする。
      inject(MatIconRegistry).setDefaultFontSetClass('material-symbols-rounded');
      // 最初の描画の前に、html の lang と日付の部品の言語を決める。
      inject(LocaleStore);
    }),
  ],
};
