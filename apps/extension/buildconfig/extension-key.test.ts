import { describe, expect, it } from 'vitest';
import { EXTENSION_ID, EXTENSION_PUBLIC_KEY, extensionIdFromPublicKey } from './extension-key';

describe('extensionIdFromPublicKey', () => {
  it('derives the fixed extension ID from the public key in the manifest', () => {
    expect(extensionIdFromPublicKey(EXTENSION_PUBLIC_KEY)).toBe(EXTENSION_ID);
  });

  it('produces a 32-letter ID in the a-p alphabet, as the API requires', () => {
    expect(EXTENSION_ID).toMatch(/^[a-p]{32}$/);
  });

  it('holds a DER SubjectPublicKeyInfo of an RSA 2048 key', () => {
    const der = Buffer.from(EXTENSION_PUBLIC_KEY, 'base64');
    // SEQUENCE { SEQUENCE { OID rsaEncryption, NULL }, BIT STRING } で始まる。
    expect(der.subarray(0, 4).toString('hex')).toBe('30820122');
    expect(der.toString('hex')).toContain('06092a864886f70d0101010500');
    expect(der.byteLength).toBe(294);
  });
});
