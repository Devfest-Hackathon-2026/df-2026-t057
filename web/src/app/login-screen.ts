import { Component, inject, signal } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatProgressSpinnerModule } from '@angular/material/progress-spinner';
import { Api } from './api';

@Component({
  selector: 'app-login-screen',
  imports: [MatButtonModule, MatProgressSpinnerModule],
  templateUrl: './login-screen.html',
  styleUrl: './login-screen.scss',
})
export class LoginScreen {
  readonly api = inject(Api);
  readonly completed = signal(false);
}
