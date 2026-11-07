import { registerLocaleData } from '@angular/common';
import ja from '@angular/common/locales/ja';
import { bootstrapApplication } from '@angular/platform-browser';
import { provideRouter } from '@angular/router';
import { routes } from './app/routes';
import { App } from './app/app';

registerLocaleData(ja);
bootstrapApplication(App, { providers: [provideRouter(routes)] }).catch(console.error);
