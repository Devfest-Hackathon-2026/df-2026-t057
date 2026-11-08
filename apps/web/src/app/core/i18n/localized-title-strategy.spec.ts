import { TestBed } from '@angular/core/testing';
import { Title } from '@angular/platform-browser';
import { Router, TitleStrategy, provideRouter } from '@angular/router';
import { LocaleStore } from './locale.store';
import { LocalizedTitleStrategy } from './localized-title-strategy';

describe('LocalizedTitleStrategy', () => {
  it('ルートの title のキーを画面の言語のタイトルにし、言語を切り替えたら付け直す', async () => {
    TestBed.configureTestingModule({
      providers: [
        provideRouter([
          { path: 'settings', title: 'settings', children: [] },
          { path: 'untitled', children: [] },
        ]),
        { provide: TitleStrategy, useClass: LocalizedTitleStrategy },
      ],
    });
    const title = TestBed.inject(Title);
    const router = TestBed.inject(Router);

    await router.navigateByUrl('/settings');
    TestBed.tick();
    expect(title.getTitle()).toBe('設定 — DOPAGAKI');

    TestBed.inject(LocaleStore).useProfileLocale('en');
    TestBed.tick();
    expect(title.getTitle()).toBe('Settings — DOPAGAKI');

    await router.navigateByUrl('/untitled');
    TestBed.tick();
    expect(title.getTitle()).toBe('Settings — DOPAGAKI');
  });
});
