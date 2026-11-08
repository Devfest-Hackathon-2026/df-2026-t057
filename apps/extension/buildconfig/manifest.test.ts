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

  it('asks for work sites only when needed', () => {
    expect(manifest.host_permissions).not.toContain('https://*/*');
    expect(manifest.optional_host_permissions).toEqual(['https://*/*', 'http://*/*']);
  });
});
