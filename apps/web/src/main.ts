import { bootstrapApplication } from '@angular/platform-browser';
import { App } from './app/app';
import { appConfig } from './app/app.config';

const start =
  location.pathname === '/extension-auth'
    ? import('./app/features/onboarding/extension-auth').then((m) =>
        bootstrapApplication(m.ExtensionAuth),
      )
    : location.pathname === '/deletion-status'
      ? import('./app/features/settings/deletion-status').then((m) =>
          bootstrapApplication(m.DeletionStatus),
        )
      : bootstrapApplication(App, appConfig);
start.catch((error: unknown) => {
  console.error(error);
});
