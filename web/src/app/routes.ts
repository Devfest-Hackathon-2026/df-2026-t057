import { Routes } from '@angular/router';
import { CalendarWorkspace } from './calendar-workspace';
import { Tasks } from './tasks';
import { Records } from './records';
import { Settings } from './settings';
import { About } from './about';

export const routes: Routes = [
  { path: '', redirectTo: 'calendar', pathMatch: 'full' },
  { path: 'calendar', component: CalendarWorkspace },
  { path: 'tasks', component: Tasks },
  { path: 'records', component: Records },
  { path: 'settings', component: Settings },
  { path: 'about', component: About },
  { path: '**', redirectTo: 'calendar' },
];
