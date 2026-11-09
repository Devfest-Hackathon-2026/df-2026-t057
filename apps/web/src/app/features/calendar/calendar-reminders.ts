import { ChangeDetectionStrategy, Component, computed, inject, linkedSignal } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatCheckboxModule } from '@angular/material/checkbox';
import { MatSnackBar } from '@angular/material/snack-bar';
import { LocaleStore } from '../../core/i18n/locale.store';
import { CalendarStore } from './calendar.store';

/** Google カレンダーの通知の上限（サーバーの calendar.MaxReminders と同じ）。 */
const maxReminders = 5;

/** 選べる通知（取りかかる時刻の何分前か）。 */
const presets: readonly number[] = [0, 10, 30, 60, 180, 300, 1440, 2880, 10080];

/** DOPAGAKI のカレンダーの通知を、取りかかる時刻の何分前に出すか選ぶ。 */
@Component({
  selector: 'app-calendar-reminders',
  imports: [MatButtonModule, MatCheckboxModule],
  template: `
    @let t = text().settings.google.reminders;
    <fieldset>
      <legend>{{ t.title }}</legend>
      <p class="hint">{{ t.lead + t.limit(max) }}</p>
      <div class="options">
        @for (minutes of options(); track minutes) {
          <mat-checkbox
            [checked]="selected().includes(minutes)"
            [disabled]="full() && !selected().includes(minutes)"
            (change)="toggle(minutes, $event.checked)"
          >
            {{ t.label(minutes) }}
          </mat-checkbox>
        }
      </div>
      <p class="hint" role="status">
        {{ chosen() === undefined ? t.notChosen : t.none }}
      </p>
      @if (calendar.reminders.error(); as error) {
        <p class="error" role="alert">{{ error.message }}</p>
      }
      <button
        mat-stroked-button
        type="button"
        (click)="save()"
        [disabled]="calendar.reminders.pending()"
      >
        {{ calendar.reminders.pending() ? t.saving : t.save }}
      </button>
    </fieldset>
  `,
  styles: `
    fieldset {
      display: grid;
      gap: 0.5rem;
      margin: 0;
      padding: 0;
      border: 0;
      min-inline-size: 0;
    }

    legend {
      padding: 0;
      font: var(--mat-sys-title-small);
    }

    .options {
      display: flex;
      flex-wrap: wrap;
      gap: 0 0.5rem;
    }

    .hint {
      margin: 0;
      color: var(--mat-sys-on-surface-variant);
    }

    .error {
      margin: 0;
      color: var(--mat-sys-error);
    }

    button {
      justify-self: start;
    }
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class CalendarReminders {
  protected readonly calendar = inject(CalendarStore);
  protected readonly text = inject(LocaleStore).web;
  private readonly snackBar = inject(MatSnackBar);
  protected readonly max = maxReminders;

  /** 保存してある通知。選んでいなければ undefined。 */
  protected readonly chosen = computed(() => this.calendar.value()?.reminders);
  protected readonly selected = linkedSignal<readonly number[]>(() => this.chosen() ?? []);
  protected readonly full = computed(() => this.selected().length >= maxReminders);
  /** 決まった選択肢に、API などで保存した別の値も加える（早い順）。 */
  protected readonly options = computed(() =>
    [...new Set([...presets, ...(this.chosen() ?? [])])].sort((a, b) => a - b),
  );

  protected toggle(minutes: number, checked: boolean): void {
    this.selected.update((current) =>
      checked
        ? [...current.filter((value) => value !== minutes), minutes]
        : current.filter((value) => value !== minutes),
    );
  }

  protected async save(): Promise<void> {
    const outcome = await this.calendar.saveReminders(this.selected());
    if (outcome.ok) {
      const text = this.text().settings;
      this.snackBar.open(text.google.reminders.saved, text.common.close, {
        duration: 3000,
      });
    }
  }
}
