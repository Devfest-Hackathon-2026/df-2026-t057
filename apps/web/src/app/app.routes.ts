import type { Routes } from '@angular/router';

export const routes: Routes = [
  { path: '', pathMatch: 'full', redirectTo: 'calendar' },
  {
    path: 'calendar',
    title: 'カレンダー — DOPAGAKI',
    loadComponent: () =>
      import('./features/calendar/workspace/calendar-workspace').then((m) => m.CalendarWorkspace),
  },
  {
    path: 'tasks',
    title: '課題 — DOPAGAKI',
    loadComponent: () => import('./features/tasks/tasks-page/tasks-page').then((m) => m.TasksPage),
  },
  {
    path: 'records',
    title: '記録 — DOPAGAKI',
    loadComponent: () =>
      import('./features/records/records-page/records-page').then((m) => m.RecordsPage),
  },
  {
    path: 'settings',
    title: '設定 — DOPAGAKI',
    loadComponent: () =>
      import('./features/settings/settings-page/settings-page').then((m) => m.SettingsPage),
  },
  {
    path: 'about',
    title: 'このアプリについて — DOPAGAKI',
    loadComponent: () => import('./features/about/about-page').then((m) => m.AboutPage),
  },
  { path: '**', redirectTo: 'calendar' },
];
