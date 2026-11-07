import { Component, computed, input, model } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MAT_DATE_LOCALE, provideNativeDateAdapter } from '@angular/material/core';
import { MatDatepickerModule } from '@angular/material/datepicker';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatSelectModule } from '@angular/material/select';

const pad = (n: number) => n.toString().padStart(2, '0');
const dateKey = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
const addDays = (d: Date, days: number) =>
  new Date(d.getFullYear(), d.getMonth(), d.getDate() + days);

/** Every 15 minutes, plus 23:59 for the common "by the end of the day" deadline. */
const TIMES = [
  ...Array.from({ length: 96 }, (_, i) => `${pad(Math.floor(i / 4))}:${pad((i % 4) * 15)}`),
  '23:59',
].sort();

/**
 * Deadline input modelled on Google Calendar: pick a day (quick buttons or a calendar popup)
 * and a time from a 15-minute list, instead of typing into a single datetime field.
 * The value stays a local `YYYY-MM-DDTHH:mm` string so callers keep their existing format.
 */
@Component({
  selector: 'app-deadline-field',
  imports: [FormsModule, MatDatepickerModule, MatFormFieldModule, MatInputModule, MatSelectModule],
  providers: [provideNativeDateAdapter(), { provide: MAT_DATE_LOCALE, useValue: 'ja-JP' }],
  styles: `
    :host {
      display: block;
    }
    fieldset {
      border: 0;
      margin: 0;
      padding: 0;
      min-width: 0;
    }
    legend {
      font-size: 12px;
      font-weight: 700;
      color: var(--muted);
      margin-bottom: 6px;
      padding: 0;
    }
    .quick {
      display: flex;
      flex-wrap: wrap;
      gap: 6px;
      margin-bottom: 10px;
    }
    .quick button {
      border: 1px solid var(--line);
      background: #fff;
      color: var(--ink);
      border-radius: 999px;
      padding: 5px 12px;
      font-size: 13px;
      cursor: pointer;
    }
    .quick button[aria-pressed='true'] {
      background: #e8eeff;
      border-color: var(--blue);
      color: var(--blue);
      font-weight: 700;
    }
    .quick button:focus-visible {
      outline: 2px solid var(--blue);
      outline-offset: 2px;
    }
    .quick button:disabled {
      opacity: 0.5;
      cursor: default;
    }
    .pickers {
      display: grid;
      grid-template-columns: minmax(0, 1fr) 128px;
      gap: 8px;
    }
    .date-input {
      cursor: pointer;
    }
    .summary {
      margin: -8px 0 4px;
      font-size: 12px;
      color: var(--muted);
    }
    .summary.past {
      color: #b3261e;
    }
  `,
  template: `
    <fieldset [disabled]="disabled()">
      <legend>締切</legend>
      <div class="quick" role="group" aria-label="締切の日付をすばやく選ぶ">
        @for (q of quick(); track q.label) {
          <button
            type="button"
            [attr.aria-pressed]="q.key === date()"
            [disabled]="disabled()"
            (click)="setDate(q.key)"
          >
            {{ q.label }}
          </button>
        }
      </div>
      <div class="pickers">
        <mat-form-field appearance="outline">
          <mat-label>締切日</mat-label>
          <input
            matInput
            class="date-input"
            [matDatepicker]="picker"
            [min]="today"
            [ngModel]="dateObject()"
            (ngModelChange)="pickDate($event)"
            (click)="picker.open()"
            [disabled]="disabled()"
            required
          />
          <mat-datepicker-toggle matIconSuffix [for]="picker" />
          <mat-datepicker #picker />
        </mat-form-field>
        <mat-form-field appearance="outline">
          <mat-label>締切時刻</mat-label>
          <mat-select
            [ngModel]="time()"
            (ngModelChange)="setTime($event)"
            [disabled]="disabled()"
            panelWidth="128px"
          >
            @for (t of times(); track t) {
              <mat-option [value]="t">{{ t }}</mat-option>
            }
          </mat-select>
        </mat-form-field>
      </div>
      @if (summary(); as s) {
        <p class="summary" [class.past]="s.past" [attr.role]="s.past ? 'alert' : null">
          {{ s.text }}
        </p>
      }
    </fieldset>
  `,
})
export class DeadlineField {
  /** Local `YYYY-MM-DDTHH:mm`. */
  readonly value = model.required<string>();
  readonly disabled = input(false);
  readonly today = addDays(new Date(), 0);

  readonly date = computed(() => this.value().slice(0, 10));
  readonly time = computed(() => this.value().slice(11, 16) || '23:59');
  readonly dateObject = computed(() => {
    const [y, m, d] = this.date().split('-').map(Number);
    return y ? new Date(y, m - 1, d) : null;
  });
  readonly times = computed(() =>
    TIMES.includes(this.time()) ? TIMES : [...TIMES, this.time()].sort(),
  );
  readonly quick = computed(() => {
    const today = new Date();
    // Upcoming Sunday; on Sunday itself, today.
    const weekend = addDays(today, (7 - today.getDay()) % 7);
    return [
      { label: '今日', key: dateKey(today) },
      { label: '明日', key: dateKey(addDays(today, 1)) },
      { label: '明後日', key: dateKey(addDays(today, 2)) },
      ...((7 - today.getDay()) % 7 > 2 ? [{ label: '今週末', key: dateKey(weekend) }] : []),
      { label: '1週間後', key: dateKey(addDays(today, 7)) },
    ];
  });
  readonly summary = computed(() => {
    const target = new Date(this.value());
    if (Number.isNaN(+target)) return null;
    const text = target.toLocaleString('ja-JP', {
      month: 'long',
      day: 'numeric',
      weekday: 'short',
      hour: '2-digit',
      minute: '2-digit',
    });
    const left = target.getTime() - Date.now();
    if (left <= 0)
      return { past: true, text: `${text} は過ぎています。未来の日時を選んでください。` };
    const hours = Math.floor(left / 3600_000);
    const remain =
      hours >= 24
        ? `あと${Math.floor(hours / 24)}日${hours % 24 ? `${hours % 24}時間` : ''}`
        : hours >= 1
          ? `あと${hours}時間`
          : `あと${Math.max(1, Math.round(left / 60_000))}分`;
    return { past: false, text: `${text} まで（${remain}）` };
  });

  setDate(key: string) {
    this.value.set(`${key}T${this.time()}`);
  }
  pickDate(date: Date | null) {
    if (date && !Number.isNaN(+date)) this.setDate(dateKey(date));
  }
  setTime(time: string) {
    this.value.set(`${this.date()}T${time}`);
  }
}
