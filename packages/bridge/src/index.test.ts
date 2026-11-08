import assert from 'node:assert/strict';
import test from 'node:test';
import { parsePageMessage } from './index.ts';

test('正しい接続メッセージを受け付ける', () => {
  const message = parsePageMessage({
    type: 'DOPAGAKI_CONNECT',
    origin: 'https://example.com',
    authorization: 'Bearer abc',
    accountId: 'u1',
  });
  assert.equal(message?.type, 'DOPAGAKI_CONNECT');
});

test('形の違うメッセージは捨てる', () => {
  assert.equal(parsePageMessage({ type: 'DOPAGAKI_CONNECT', origin: 'x' }), undefined);
  assert.equal(parsePageMessage('DOPAGAKI_DISCONNECT'), undefined);
  assert.equal(parsePageMessage({ type: 'OTHER' }), undefined);
});
