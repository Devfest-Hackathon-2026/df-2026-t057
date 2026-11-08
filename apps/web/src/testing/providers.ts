import { provideNativeDateAdapter } from '@angular/material/core';

// LocaleStore は日付の部品の言語も切り替えるので、どのテストでも DateAdapter を用意する（本番は app.config.ts で用意している）。
export default [provideNativeDateAdapter()];
