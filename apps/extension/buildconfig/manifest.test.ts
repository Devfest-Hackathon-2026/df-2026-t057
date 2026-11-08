import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { EXTENSION_PUBLIC_KEY } from './extension-key';
import { YOUTUBE_MATCH, managementMatches, manifestFor } from './manifest';

describe('manifestFor', () => {
  const manifest = manifestFor('https://release.example', '1.2.3');

  it('uses the release version and the production origin', () => {
    expect(manifest.version).toBe('1.2.3');
    expect(manifest.host_permissions).toEqual(['https://release.example/*', YOUTUBE_MATCH]);
    expect(managementMatches('https://release.example')).toEqual(['https://release.example/*']);
  });

  it('fixes the extension ID with the public key', () => {
    expect(manifest.key).toBe(EXTENSION_PUBLIC_KEY);
  });

  it('takes the name and description from every locale', () => {
    expect(manifest.default_locale).toBe('en');
    for (const locale of ['en', 'ja']) {
      const file = new URL(`../public/_locales/${locale}/messages.json`, import.meta.url);
      const messages = JSON.parse(readFileSync(file, 'utf8')) as Record<string, unknown>;
      for (const value of [manifest.name, manifest.description, manifest.action.default_title]) {
        const key = /^__MSG_(\w+)__$/.exec(value)?.[1];
        expect(key && messages[key], `${locale}: ${value}`).toBeTruthy();
      }
    }
  });

  it('asks for work sites only when needed', () => {
    expect(manifest.host_permissions).not.toContain('https://*/*');
    expect(manifest.optional_host_permissions).toEqual(['https://*/*', 'http://*/*']);
  });
});
