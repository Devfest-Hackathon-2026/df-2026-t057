import { describe, expect, it } from 'vitest';
import { DEFAULT_APP_URL, resolveManagementOrigin, resolveVersion } from './release';

describe('resolveVersion', () => {
  it('uses the release tag without the leading v', () => {
    expect(resolveVersion('v1.2.3', '0.1.0')).toBe('1.2.3');
    expect(resolveVersion('65535.0.1', '0.1.0')).toBe('65535.0.1');
  });

  it('falls back to the package version when no tag is given', () => {
    expect(resolveVersion(undefined, '0.1.0')).toBe('0.1.0');
    expect(resolveVersion('', '0.1.0')).toBe('0.1.0');
  });

  it.each(['v1.2.3-beta.1', 'v01.2.3', 'v65536.0.0', 'v0.0.0', 'v1.2', 'latest', 'v1.2.3.4'])(
    'rejects %s',
    (tag) => {
      expect(() => resolveVersion(tag, '0.1.0')).toThrow(/X\.Y\.Z/);
    },
  );

  it('rejects an invalid package version', () => {
    expect(() => resolveVersion(undefined, '0.0.0')).toThrow();
  });
});

describe('resolveManagementOrigin', () => {
  it('defaults to the local management app', () => {
    expect(resolveManagementOrigin(undefined)).toBe(new URL(DEFAULT_APP_URL).origin);
    expect(resolveManagementOrigin('')).toBe('http://127.0.0.1:4200');
  });

  it('keeps only the origin of the production URL', () => {
    expect(resolveManagementOrigin('https://release.example/some/path?x=1')).toBe(
      'https://release.example',
    );
  });

  it.each(['not a url', 'ftp://example.com', 'chrome-extension://abc'])('rejects %s', (value) => {
    expect(() => resolveManagementOrigin(value)).toThrow(/APP_URL/);
  });
});
