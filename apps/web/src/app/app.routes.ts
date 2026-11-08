import type { Routes } from '@angular/router';
import type { PageTitleKey } from './core/i18n/localized-title-strategy';

// title には文言ではなくキーを書く。LocalizedTitleStrategy が画面の言語のタイトルにする。
export const routes: Routes = [
  { path: '', pathMatch: 'full', redirectTo: 'calendar' },
  {
    path: 'calendar',
    title: 'calendar' satisfies PageTitleKey,
    loadComponent: () =>
      import('./features/calendar/workspace/calendar-workspace').then((m) => m.CalendarWorkspace),
  },
  {
    path: 'tasks',
    title: 'tasks' satisfies PageTitleKey,
    loadComponent: () => import('./features/tasks/tasks-page/tasks-page').then((m) => m.TasksPage),
  },
  {
    path: 'records',
    title: 'records' satisfies PageTitleKey,
    loadComponent: () =>
      import('./features/records/records-page/records-page').then((m) => m.RecordsPage),
  },
  {
    path: 'settings',
    title: 'settings' satisfies PageTitleKey,
    loadComponent: () =>
      import('./features/settings/settings-page/settings-page').then((m) => m.SettingsPage),
  },
  {
    path: 'about',
    title: 'about' satisfies PageTitleKey,
    loadComponent: () => import('./features/about/about-page').then((m) => m.AboutPage),
  },
  { path: '**', redirectTo: 'calendar' },
];
