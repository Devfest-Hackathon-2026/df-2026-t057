import { TestBed } from '@angular/core/testing';
import { DOCUMENT } from '@angular/core';
import { DateAdapter } from '@angular/material/core';
import { messages } from '@dopagaki/messages';
import { AccountScope } from '../auth/account-scope';
import { LocaleStore } from './locale.store';

describe('LocaleStore', () => {
  function setup(): { store: LocaleStore; document: Document; dateAdapter: DateAdapter<Date> } {
    return {
      store: TestBed.inject(LocaleStore),
      document: TestBed.inject(DOCUMENT),
      dateAdapter: TestBed.inject<DateAdapter<Date>>(DateAdapter),
    };
  }

  it('プロフィールを読むまでは、ブラウザーの言語を使う', () => {
    const { store } = setup();
    // テストの setup で navigator.languages を ja-JP にしている。
    expect(store.browserLocale).toBe('ja');
    expect(store.locale()).toBe('ja');
    expect(store.web()).toBe(messages.ja.web);
  });

  it('プロフィールの言語に切り替えると、文言・html の lang・日付の部品の言語が変わる', () => {
    const { store, document, dateAdapter } = setup();
    const setLocale = vi.spyOn(dateAdapter, 'setLocale');
    store.useProfileLocale('en');
    TestBed.tick();
    expect(store.locale()).toBe('en');
    expect(store.shared()).toBe(messages.en.shared);
    expect(store.web()).toBe(messages.en.web);
    expect(document.documentElement.lang).toBe('en');
    expect(setLocale).toHaveBeenLastCalledWith('en-US');
  });

  it('アカウントが切り替わったら、ブラウザーの言語に戻す', () => {
    const { store } = setup();
    store.useProfileLocale('en');
    TestBed.inject(AccountScope).reset();
    expect(store.locale()).toBe('ja');
  });
});
