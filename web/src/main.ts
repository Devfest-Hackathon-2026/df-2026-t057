import { registerLocaleData } from '@angular/common';
import ja from '@angular/common/locales/ja';
import { bootstrapApplication } from '@angular/platform-browser';
import { App } from './app/app';

registerLocaleData(ja);
bootstrapApplication(App).catch(console.error);
