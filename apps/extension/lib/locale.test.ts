import { messages } from '@dopagaki/messages';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { currentLocale, currentMessages, followLocale, messagesFor } from './locale';
import { emptyState, type ExtensionState } from './schemas';

const english: ExtensionState = { ...emptyState, profile: { locale: 'en', revision: 1 } };

describe('locale', () => {
  beforeEach(() => {
    vi.stubGlobal('navigator', { languages: ['ja-JP', 'en-US'] });
  });

  afterEach(() => {
    followLocale(null);
    vi.unstubAllGlobals();
  });

  it('uses the browser language until the profile arrives', () => {
    expect(messagesFor(undefined)).toBe(messages.ja);
    expect(messagesFor(emptyState).extension.close).toBe('閉じる');
    expect(currentLocale()).toBe('ja');
  });

  it('switches to the language of the profile', () => {
    expect(messagesFor(english).extension.close).toBe('Close');
    followLocale(english);
    expect(currentLocale()).toBe('en');
    expect(currentMessages().extension.pause).toBe('Pause');
  });

  it('goes back to the browser language after disconnecting', () => {
    followLocale(english);
    followLocale(null);
    expect(currentMessages()).toBe(messages.ja);
  });
});
