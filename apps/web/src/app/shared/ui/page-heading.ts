import { ChangeDetectionStrategy, Component, input } from '@angular/core';

/** ページの見出し（小さな英字・見出し・ひとこと）。 */
@Component({
  selector: 'app-page-heading',
  template: `
    @if (eyebrow()) {
      <p class="eyebrow">{{ eyebrow() }}</p>
    }
    <h1>
      {{ heading() }}
      <span class="dot" aria-hidden="true">.</span>
    </h1>
    @if (lead()) {
      <p class="lead">{{ lead() }}</p>
    }
  `,
  styles: `
    :host {
      display: block;
      margin-bottom: 1.5rem;
    }

    .eyebrow {
      margin: 0 0 0.5rem;
      color: var(--mat-sys-on-surface-variant);
      font: var(--mat-sys-label-medium);
      letter-spacing: 0.15em;
    }

    h1 {
      margin: 0 0 0.25rem;
      font: var(--mat-sys-headline-large);
    }

    .dot {
      color: var(--mat-sys-primary);
    }

    .lead {
      margin: 0;
      color: var(--mat-sys-on-surface-variant);
    }
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class PageHeading {
  readonly eyebrow = input('');
  readonly heading = input.required<string>();
  readonly lead = input('');
}
