import { Component } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { FormControl, ReactiveFormsModule } from '@angular/forms';
import { provideNativeDateAdapter } from '@angular/material/core';
import { messages } from '@dopagaki/messages';
import { DeadlineField, quickDates, remainingLabel } from './deadline-field';

const text = messages.ja.web.tasks.deadlineField;

@Component({
  imports: [DeadlineField, ReactiveFormsModule],
  template: `
    <app-deadline-field [formControl]="control" />
  `,
})
class Host {
  readonly control = new FormControl('', { nonNullable: true });
}

describe('quickDates', () => {
  it('月〜木曜は「今週末」（次の日曜）を出す', () => {
    const monday = new Date(2026, 10, 9);
    expect(quickDates(monday, text).map((item) => item.label)).toEqual([
      '今日',
      '明日',
      '明後日',
      '今週末',
      '1週間後',
    ]);
    expect(quickDates(monday, text).find((item) => item.label === '今週末')?.key).toBe(
      '2026-11-15',
    );
  });

  it('金〜日曜は「今週末」を出さない', () => {
    expect(quickDates(new Date(2026, 10, 13), text).some((item) => item.label === '今週末')).toBe(
      false,
    );
    expect(quickDates(new Date(2026, 10, 15), text).some((item) => item.label === '今週末')).toBe(
      false,
    );
  });
});

describe('remainingLabel', () => {
  it('日・時間・分で表す', () => {
    expect(remainingLabel(26 * 3_600_000, text)).toBe('あと1日2時間');
    expect(remainingLabel(48 * 3_600_000, text)).toBe('あと2日');
    expect(remainingLabel(3 * 3_600_000 + 10, text)).toBe('あと3時間');
    expect(remainingLabel(20_000, text)).toBe('あと1分');
  });
});

describe('DeadlineField', () => {
  beforeEach(() => {
    TestBed.configureTestingModule({ providers: [provideNativeDateAdapter()] });
  });

  it('すばやい選択で日付を変え、時刻は保つ', async () => {
    const fixture = TestBed.createComponent(Host);
    fixture.componentInstance.control.setValue('2099-01-01T18:00');
    await fixture.whenStable();
    const element = fixture.nativeElement as HTMLElement;
    const tomorrow = [...element.querySelectorAll('button')].find(
      (button) => button.textContent.trim() === '明日',
    );
    tomorrow?.click();
    await fixture.whenStable();
    const value = fixture.componentInstance.control.value;
    expect(value.endsWith('T18:00')).toBe(true);
    expect(tomorrow?.getAttribute('aria-pressed')).toBe('true');
    expect(element.textContent).toContain('18:00 まで（あと');
  });

  it('過ぎた締切を知らせる', async () => {
    const fixture = TestBed.createComponent(Host);
    fixture.componentInstance.control.setValue('2020-01-01T09:00');
    await fixture.whenStable();
    const alert = (fixture.nativeElement as HTMLElement).querySelector('[role="alert"]');
    expect(alert?.textContent).toContain('は過ぎています。未来の日時を選んでください。');
  });

  it('任意の分をフォームへ渡し、不正な時刻ではフォームを無効にする', async () => {
    const fixture = TestBed.createComponent(Host);
    const control = fixture.componentInstance.control;
    control.setValue('2099-01-01T12:15');
    await fixture.whenStable();
    const input = (fixture.nativeElement as HTMLElement).querySelector<HTMLInputElement>(
      'input[role="combobox"]',
    );
    expect(input).not.toBeNull();
    if (!input) return;
    for (const [time, valid] of [
      ['12:17', true],
      ['25:61', false],
      ['', false],
      ['23:59', true],
    ] as const) {
      input.value = time;
      input.dispatchEvent(new Event('input', { bubbles: true }));
      input.dispatchEvent(new Event('blur'));
      await fixture.whenStable();
      expect(control.value).toBe(`2099-01-01T${time}`);
      expect(control.valid).toBe(valid);
      if (!valid) {
        expect(
          (fixture.nativeElement as HTMLElement).querySelector('mat-error')?.textContent,
        ).toContain('00:00〜23:59で入力してください');
      }
    }
    control.setValue('2099-01-01T12:17');
    control.disable();
    await fixture.whenStable();
    expect(input.value).toBe('12:17');
    expect(input.disabled).toBe(true);
  });
});
