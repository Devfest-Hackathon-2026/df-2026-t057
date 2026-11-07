import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, writeFileSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';

function deploy(failure = '', image = 'registry/app@sha256:abc') {
  const dir = mkdtempSync(join(tmpdir(), 'dopagaki-deploy-'));
  const log = join(dir, 'calls');
  const mock = `#!${process.execPath}
import { appendFileSync, readFileSync } from 'node:fs';
const args = process.argv.slice(2);
const command = process.argv[1].endsWith('gcloud') ? 'gcloud' : 'curl';
appendFileSync(process.env.CALL_LOG, JSON.stringify({command,args})+'\\n');
if (command === 'gcloud') {
  if (args.includes('describe')) {
    const calls = readFileSync(process.env.CALL_LOG,'utf8').split('\\n').filter(Boolean).map(JSON.parse);
    const count = calls.filter(c => c.command === 'gcloud' && c.args.includes('describe')).length;
    console.log(JSON.stringify({status:{traffic:count === 1
      ? [{revisionName:'old-a',percent:60},{revisionName:'old-b',percent:40}]
      : [{tag:'cd-candidate',url:'https://candidate.example'}]}}));
  } else if (args.includes('update')) console.log('new-revision');
} else {
  const url = args.at(-1);
  const phase = url.includes('candidate') ? 'candidate' : 'production';
  const health = url.endsWith('/health');
  const fail = process.env.FAILURE === phase + (health ? '-health' : '-auth');
  if (health) { if (fail) process.exit(22); console.log('{"status":"ok"}'); }
  else process.stdout.write(fail ? '200' : '401');
}
`;
  for (const command of ['gcloud', 'curl']) {
    writeFileSync(join(dir, command), mock, { mode: 0o755 });
  }
  writeFileSync(log, '');
  try {
    const result = spawnSync('bash', [resolve('deploy/cloud-run.sh'), image], {
      env: {
        ...process.env,
        PATH: `${dir}:${process.env.PATH}`,
        CALL_LOG: log,
        FAILURE: failure,
        PROJECT_ID: 'test-project',
        REGION: 'test-region',
        SERVICE: 'test-service',
        SERVICE_URL: 'https://production.example',
        GITHUB_STEP_SUMMARY: join(dir, 'summary'),
      },
      encoding: 'utf8',
    });
    const calls: { command: string; args: string[] }[] = readFileSync(log, 'utf8')
      .split('\n')
      .filter(Boolean)
      .map((line) => JSON.parse(line));
    return { result, calls };
  } finally {
    rmSync(dir, { recursive: true });
  }
}

test('production traffic changes only after candidate health and auth checks pass', () => {
  const { result, calls } = deploy();
  assert.equal(result.status, 0, result.stderr);
  const promotion = calls.findIndex((c) => c.args.includes('--to-revisions=new-revision=100'));
  const before = calls.slice(0, promotion);
  assert.equal(before.filter((c) => c.command === 'curl').length, 2);
  assert.ok(before.some((c) => c.args.includes('--no-traffic')));
  assert.equal(calls.filter((c) => c.args.includes('update-traffic')).length, 1);
});

for (const failure of ['candidate-health', 'candidate-auth']) {
  test(`${failure} failure leaves production traffic unchanged`, () => {
    const { result, calls } = deploy(failure);
    assert.notEqual(result.status, 0);
    assert.ok(!calls.some((c) => c.args.includes('update-traffic')));
  });
}
for (const failure of ['production-health', 'production-auth']) {
  test(`${failure} failure restores the previous traffic split`, () => {
    const { result, calls } = deploy(failure);
    assert.notEqual(result.status, 0);
    const changes = calls.filter((c) => c.args.includes('update-traffic'));
    assert.equal(changes.length, 2);
    assert.ok(changes[1].args.includes('--to-revisions=old-a=60,old-b=40'));
  });
}
test('mutable image tags are rejected before any cloud call', () => {
  const { result, calls } = deploy('', 'registry/app:latest');
  assert.notEqual(result.status, 0);
  assert.equal(calls.length, 0);
});
