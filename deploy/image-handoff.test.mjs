import assert from 'node:assert/strict';
import { test } from 'node:test';
import { mkdtempSync, writeFileSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';

function fixture() {
  const dir = mkdtempSync(join(tmpdir(), 'dopagaki-image-'));
  const log = join(dir, 'calls');
  const output = join(dir, 'output');
  writeFileSync(log, '');
  writeFileSync(output, '');
  writeFileSync(
    join(dir, 'docker'),
    `#!${process.execPath}
import { appendFileSync } from 'node:fs';
const args = process.argv.slice(2);
appendFileSync(process.env.CALL_LOG, JSON.stringify(args)+'\\n');
if (args[0] === 'image') console.log('sha256:test-image');
if (args[0] === 'save') process.stdout.write('tested-image');
if (args[0] === 'load') {
  let body = ''; for await (const chunk of process.stdin) body += chunk;
  if (body !== 'tested-image') process.exit(2);
}
`,
    { mode: 0o755 },
  );
  writeFileSync(
    join(dir, 'gh'),
    `#!${process.execPath}
if (process.env.API_FAILURE) process.exit(1);
console.log(process.env.LATEST_SHA);
`,
    { mode: 0o755 },
  );
  const env = {
    ...process.env,
    PATH: `${dir}:${process.env.PATH}`,
    CALL_LOG: log,
    GITHUB_OUTPUT: output,
    GITHUB_SHA: 'test-sha',
    GITHUB_REPOSITORY: 'test/repo',
    IMAGE: 'registry.example/app',
  };
  const run = (script, overrides = {}) =>
    spawnSync('bash', [resolve(import.meta.dirname, script)], {
      cwd: dir,
      env: { ...env, ...overrides },
      encoding: 'utf8',
    });
  return { dir, log, output, run };
}

for (const expected of ['sha256:test-image', 'sha256:wrong-image']) {
  test(`image transfer only tags the exact tested image: ${expected}`, () => {
    const f = fixture();
    try {
      const saved = f.run('save-image.sh');
      assert.equal(saved.status, 0, saved.stderr);
      assert.equal(readFileSync(f.output, 'utf8'), 'image-id=sha256:test-image\n');
      const loaded = f.run('load-image.sh', { EXPECTED_IMAGE_ID: expected });
      const calls = readFileSync(f.log, 'utf8').trim().split('\n').map(JSON.parse);
      if (expected === 'sha256:test-image') {
        assert.equal(loaded.status, 0, loaded.stderr);
        assert.deepEqual(calls.at(-1), [
          'tag',
          'dopagaki:test-sha',
          'registry.example/app:test-sha',
        ]);
      } else {
        assert.notEqual(loaded.status, 0);
        assert.ok(!calls.some((args) => args[0] === 'tag'));
      }
    } finally {
      rmSync(f.dir, { recursive: true });
    }
  });
}

test('a corrupt image archive fails before tagging or publishing', () => {
  const f = fixture();
  try {
    assert.equal(f.run('save-image.sh').status, 0);
    writeFileSync(join(f.dir, 'dist/production-image.tar.gz'), 'corrupt');
    assert.notEqual(f.run('load-image.sh', { EXPECTED_IMAGE_ID: 'sha256:test-image' }).status, 0);
    assert.ok(!readFileSync(f.log, 'utf8').includes('["tag"'));
  } finally {
    rmSync(f.dir, { recursive: true });
  }
});

for (const latest of ['test-sha', 'newer-sha']) {
  test(`the current-main guard distinguishes ${latest}`, () => {
    const f = fixture();
    try {
      const result = f.run('current-main.sh', { LATEST_SHA: latest });
      assert.equal(result.status, 0, result.stderr);
      assert.equal(readFileSync(f.output, 'utf8'), `current=${latest === 'test-sha'}\n`);
    } finally {
      rmSync(f.dir, { recursive: true });
    }
  });
}

test('the current-main guard fails closed if GitHub cannot be queried', () => {
  const f = fixture();
  try {
    assert.notEqual(f.run('current-main.sh', { API_FAILURE: '1' }).status, 0);
    assert.equal(readFileSync(f.output, 'utf8'), '');
  } finally {
    rmSync(f.dir, { recursive: true });
  }
});
