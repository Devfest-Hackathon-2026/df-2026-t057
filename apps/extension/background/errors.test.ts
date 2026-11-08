import { messages } from '@dopagaki/messages';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { emptyState, type ExtensionState } from '../lib/schemas';
import { errorReply, ExtensionError } from './errors';

const storedState = vi.hoisted(() => vi.fn());
vi.mock('./store', () => ({ getState: storedState }));

const english: ExtensionState = { ...emptyState, profile: { locale: 'en', revision: 1 } };

describe('errorReply', () => {
  beforeEach(() => {
    vi.stubGlobal('navigator', { languages: ['ja-JP'] });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('shows the reason in the language of the profile', async () => {
    storedState.mockResolvedValue(english);
    expect(await errorReply(new ExtensionError('authTimedOut'))).toEqual({
      ok: false,
      error: messages.en.extension.authTimedOut,
      retryable: false,
    });
  });

  it('uses the browser language before connecting', async () => {
    storedState.mockResolvedValue(undefined);
    const reply = await errorReply(new ExtensionError('connectFromPopup'));
    expect(reply.error).toBe('拡張のポップアップから同じGoogleアカウントで接続してください。');
  });

  it('hides unexpected failures behind the generic message', async () => {
    storedState.mockResolvedValue(english);
    const reply = await errorReply(new Error('raw detail'));
    expect(reply.error).toBe(messages.en.shared.problems.internal);
  });
});
