import test from 'node:test';
import assert from 'node:assert/strict';
import { build } from 'esbuild';
import vm from 'node:vm';

test('extension retries configuration after offline startup and gets fresh SDK tokens without the web page', async () => {
  const fixture = { fail: true, reads: 0, token: 0, persistence: undefined as unknown };
  const result = await build({
    entryPoints: ['extension/src/auth.ts'],
    bundle: true,
    write: false,
    format: 'cjs',
    platform: 'node',
    plugins: [
      {
        name: 'firebase-session-fixture',
        setup(b) {
          b.onResolve({ filter: /^firebase\// }, ({ path }) => ({ path, namespace: 'fixture' }));
          b.onLoad({ filter: /.*/, namespace: 'fixture' }, ({ path }) => ({
            contents:
              path === 'firebase/app'
                ? 'exports.initializeApp=x=>x;'
                : `exports.indexedDBLocalPersistence='persistent-sdk-store';exports.initializeAuth=(_,options)=>{fixture.persistence=options.persistence;return {authStateReady:async()=>{},currentUser:{uid:'owner',email:'owner@example.test',getIdToken:async()=> 'fresh-'+(++fixture.token)}}};exports.signOut=async()=>{};exports.GoogleAuthProvider={};exports.signInWithCredential=async()=>{};`,
          }));
        },
      },
    ],
  });
  const module = { exports: {} as any };
  vm.runInNewContext(result.outputFiles[0].text, {
    module,
    exports: module.exports,
    fixture,
    fetch: async () => {
      fixture.reads++;
      if (fixture.fail) throw new Error('offline');
      return { ok: true, json: async () => ({ mode: 'live', firebase: { apiKey: 'fixture' } }) };
    },
  });
  await assert.rejects(module.exports.extensionConnection(), /offline/);
  fixture.fail = false;
  const first = await module.exports.extensionConnection();
  const second = await module.exports.extensionConnection();
  assert.equal(first.accountId, 'owner');
  assert.equal(first.auth, 'Bearer fresh-1');
  assert.equal(second.auth, 'Bearer fresh-2');
  assert.equal(fixture.reads, 2);
  assert.equal(fixture.persistence, 'persistent-sdk-store');
});
