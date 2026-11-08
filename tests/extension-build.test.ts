import test from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { cp, mkdir, mkdtemp, readFile, readdir, rm, symlink, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

test('extension release uses the tag and production origin, removes stale files and rejects invalid versions', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'dopagaki-build-'));
  try {
    for (const path of [
      'scripts/build-extension.mjs',
      'extension',
      'shared',
      'web/public/gold-light.png',
      'package.json',
    ]) {
      await cp(path, join(directory, path), { recursive: true });
    }
    await symlink(resolve('node_modules'), join(directory, 'node_modules'), 'dir');
    await mkdir(join(directory, 'dist/extension'), { recursive: true });
    await writeFile(join(directory, 'dist/extension/stale.txt'), 'must not ship');
    const origin = 'https://release.example';
    const build = (tag?: string) => {
      const env = { ...process.env, APP_URL: origin };
      delete env.RELEASE_TAG;
      if (tag !== undefined) env.RELEASE_TAG = tag;
      return spawnSync(process.execPath, ['scripts/build-extension.mjs'], {
        cwd: directory,
        env,
        encoding: 'utf8',
      });
    };
    const result = build('v1.2.3');
    assert.equal(result.status, 0, result.stderr);
    const output = join(directory, 'dist/extension');
    const manifest = JSON.parse(await readFile(join(output, 'manifest.json'), 'utf8'));
    assert.equal(manifest.version, '1.2.3');
    assert.deepEqual(manifest.host_permissions, [
      origin + '/*',
      'https://www.youtube.com/*',
      'https://identitytoolkit.googleapis.com/*',
      'https://securetoken.googleapis.com/*',
    ]);
    assert.deepEqual(manifest.content_scripts[0].matches, [
      origin + '/*',
      'https://www.youtube.com/*',
    ]);
    assert(!(await readdir(output)).includes('stale.txt'));
    for (const file of ['background.js', 'content.js', 'popup.js']) {
      const code = await readFile(join(output, file), 'utf8');
      assert(code.includes(origin), file);
      assert(!code.includes('__MANAGEMENT_ORIGIN__'), file);
      assert(!code.includes('http://127.0.0.1:4200'), file);
    }
    for (const tag of ['v1.2.3-beta.1', 'v01.2.3', 'v65536.0.0', 'v0.0.0']) {
      assert.notEqual(build(tag).status, 0, tag);
    }
    assert.equal(build().status, 0);
    const local = JSON.parse(await readFile(join(output, 'manifest.json'), 'utf8'));
    const pkg = JSON.parse(await readFile('package.json', 'utf8'));
    assert.equal(local.version, pkg.version);
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});
