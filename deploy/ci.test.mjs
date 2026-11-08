import assert from 'node:assert/strict';
import { readFileSync, readdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';
import { execFile, spawnSync } from 'node:child_process';
import { createServer } from 'node:http';
import { promisify } from 'node:util';
import { runInNewContext } from 'node:vm';
import { load } from 'js-yaml';
import picomatch from 'picomatch';
import { planChecks } from './ci-policy.mjs';

const workflow = load(
  readFileSync(new URL('../.github/workflows/check.yml', import.meta.url), 'utf8'),
);
const cd = load(readFileSync(new URL('../.github/workflows/cd.yml', import.meta.url), 'utf8'));
const filters = load(readFileSync(new URL('../.github/ci-paths.yaml', import.meta.url), 'utf8'));

function selection(
  files,
  event = 'pull_request',
  outcomes = {},
  current = 'true',
  redeploy = false,
) {
  const flags = Object.fromEntries(
    Object.entries(filters).map(([key, patterns]) => [
      key,
      files.some(picomatch(patterns.flat(Infinity), { dot: true })),
    ]),
  );
  const plan = planChecks(files, flags, event, redeploy);
  const outputs = Object.fromEntries(
    Object.entries(plan).map(([key, value]) => [key, String(value)]),
  );
  const needs = { changes: { outputs, result: outcomes.changes ?? 'success' } };
  const context = {
    github: { event_name: event, ref: event === 'push' ? 'refs/heads/main' : 'refs/pull/1/merge' },
    needs,
    steps: { e2e: { outcome: 'success' } },
    failure: () => false,
    cancelled: () => false,
    always: () => true,
  };
  const selected = [];
  for (const [id, job] of Object.entries(workflow.jobs)) {
    if (id === 'changes') continue;
    const implicitSuccess =
      /(?:always|cancelled|failure|success)\(/.test(job.if ?? '') ||
      [job.needs].flat().every((name) => needs[name].result === 'success');
    if (!implicitSuccess || (job.if && !runInNewContext(job.if, context))) {
      needs[id] = { result: 'skipped' };
      continue;
    }
    selected.push(
      ...(job.steps ?? []).filter((step) => !step.if || runInNewContext(step.if, context)),
    );
    let result = outcomes[id] ?? 'success';
    if (id === 'check') {
      const gate = spawnSync('bash', ['-c', job.steps[0].run], {
        env: { ...process.env, RESULTS: job.needs.map((name) => needs[name].result).join(',') },
      });
      result = gate.status === 0 ? 'success' : 'failure';
    }
    needs[id] = { result, outputs: id === 'check' ? { current } : {} };
  }
  return {
    plan,
    selected,
    results: Object.fromEntries(Object.entries(needs).map(([key, value]) => [key, value.result])),
  };
}

const cases = [
  ['README.md', 'none', false, false, false],
  ['apps/web/src/app/features/tasks/tasks-page/tasks-page.html', 'smoke', true, false, false],
  ['apps/extension/content/notice.ts', 'extension-smoke', false, false, false],
  ['apps/api/internal/wallet/domain.go', 'full', true, false, false],
  ['apps/api/internal/task/domain.go', 'smoke', true, false, false],
  ['apps/web/src/app/core/auth/auth.service.ts', 'full', true, false, false],
  ['apps/extension/background/auth.ts', 'full', false, false, false],
  ['packages/bridge/src/index.ts', 'full', true, false, false],
  ['packages/messages/src/ja/web.ts', 'smoke', true, false, false],
  ['packages/contract/openapi.yaml', 'full', true, false, true],
  ['packages/tokens/src/tokens.css', 'full', true, false, true],
  ['apps/api/internal/wallet/domain_test.go', 'none', false, false, false],
  ['apps/web/src/app/app.spec.ts', 'none', false, false, false],
  ['e2e/tests/app.spec.ts', 'full', false, false, false],
  ['Dockerfile', 'full', true, true, false],
  ['pnpm-lock.yaml', 'full', true, true, true],
  ['apps/api/go.mod', 'full', true, true, true],
  ['.github/workflows/check.yml', 'full', false, true, false],
  ['.github/workflows/cd.yml', 'full', true, true, false],
  ['deploy/cloud-run.sh', 'none', true, false, false],
  ['deploy/ci.test.mjs', 'none', false, false, false],
  ['deploy/current-main.sh', 'full', false, true, false],
  ['deploy/save-image.sh', 'full', false, true, false],
  ['deploy/load-image.sh', 'full', false, true, false],
];
for (const [path, suite, deploy, image, generate] of cases) {
  test(`${path}: PR selects ${suite} with the expected deployment/build/generation scope`, () => {
    const { plan, results } = selection([path]);
    assert.equal(plan.suite, suite);
    assert.equal(plan.deploy, deploy);
    assert.equal(plan.image, image);
    assert.equal(plan.generate, generate);
    assert.equal(results.deploy, 'skipped');
    assert.equal(results.check, 'success');
  });
}

test('Terraform PR validates the configuration without app checks, E2E or deployment', () => {
  const { plan, results } = selection(['deploy/terraform/run.tf']);
  assert.equal(results.terraform, 'success');
  assert.equal(results.api, 'skipped');
  assert.equal(results.e2e, 'skipped');
  assert.equal(results.check, 'success');
  assert.equal(plan.deploy, false);
  assert.equal(plan.image, false);
});

test('app PR skips Terraform validation', () => {
  assert.equal(selection(['apps/api/internal/wallet/domain.go']).results.terraform, 'skipped');
});

test('failed Terraform validation blocks the required check', () => {
  assert.equal(
    selection(['deploy/terraform/run.tf'], 'pull_request', { terraform: 'failure' }).results.check,
    'failure',
  );
});

for (const path of [
  'apps/web/src/app/app.ts',
  'apps/extension/content/notice.ts',
  'apps/api/internal/task/domain.go',
]) {
  test(`main keeps full browser validation for ${path}`, () => {
    const { plan } = selection([path], 'push');
    assert.equal(plan.suite, 'full');
    assert.equal(plan.image, plan.deploy);
  });
}

test('CI-only main changes do not trigger browser tests or deploy an image', () => {
  const { plan, results } = selection(
    ['.github/workflows/check.yml', 'deploy/ci-policy.mjs'],
    'push',
  );
  assert.equal(plan.suite, 'none');
  assert.equal(plan.image, false);
  assert.equal(results.deploy, 'skipped');
});

test('manual full validation has no deployment or schedule', () => {
  const { plan, results } = selection([], 'workflow_dispatch');
  assert.equal(plan.suite, 'full');
  assert.equal(results.api, 'success');
  assert.equal(results.frontend, 'success');
  assert.equal(results.e2e, 'success');
  assert.equal(results.deploy, 'skipped');
  assert.equal(workflow.on.schedule, undefined);
  assert.deepEqual(Object.keys(cd.on), ['workflow_call']);
});

test('an explicit push redeploy validates all code and generates a tested production image', () => {
  const { plan, results } = selection([], 'push', {}, 'true', true);
  for (const key of ['api', 'web', 'extension', 'shared', 'generate', 'image', 'deploy'])
    assert.equal(plan[key], true, key);
  assert.equal(plan.suite, 'full');
  for (const job of ['api', 'frontend', 'e2e', 'check', 'deploy'])
    assert.equal(results[job], 'success', job);
  assert.equal(selection([], 'push', {}, 'false', true).results.deploy, 'skipped');
});

for (const event of ['pull_request', 'workflow_dispatch']) {
  test(`${event} cannot request a production redeploy`, () => {
    const { plan, results } = selection([], event, {}, 'true', true);
    assert.equal(plan.image, false);
    assert.equal(plan.deploy, false);
    assert.equal(results.deploy, 'skipped');
  });
}

test('the workflow accepts explicit redeploy requests only from the main push commit', () => {
  const env = workflow.jobs.changes.steps.find((step) => step.id === 'policy').env;
  assert.equal(
    env.REQUEST_DEPLOY,
    "${{ github.event_name == 'push' && contains(github.event.head_commit.message, '[deploy]') }}",
  );
});

test('the policy entry point passes an explicit deployment request into Actions outputs', (t) => {
  const dir = mkdtempSync(join(tmpdir(), 'dopagaki-redeploy-'));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  const output = join(dir, 'output');
  const run = spawnSync(process.execPath, [new URL('./ci-policy.mjs', import.meta.url).pathname], {
    env: {
      ...process.env,
      CHANGES: JSON.stringify({ all_files: '[]' }),
      EVENT_NAME: 'push',
      REQUEST_DEPLOY: 'true',
      GITHUB_OUTPUT: output,
    },
    encoding: 'utf8',
  });
  assert.equal(run.status, 0, run.stderr);
  const outputs = Object.fromEntries(
    readFileSync(output, 'utf8')
      .trim()
      .split('\n')
      .map((line) => line.split('=')),
  );
  for (const key of ['api', 'web', 'extension', 'shared', 'generate', 'image', 'deploy'])
    assert.equal(outputs[key], 'true', key);
  assert.equal(outputs.suite, 'full');
});

for (const job of ['changes', 'api', 'frontend', 'e2e']) {
  for (const result of ['failure', 'cancelled']) {
    test(`${job} ${result} fails check and blocks CD`, () => {
      const { results } = selection(['apps/api/internal/wallet/domain.go'], 'push', {
        [job]: result,
      });
      assert.equal(results.check, 'failure');
      assert.equal(results.deploy, 'skipped');
      if (job !== 'e2e') assert.equal(results.e2e, 'skipped');
      const requested = selection([], 'push', { [job]: result }, 'true', true);
      assert.equal(requested.results.check, 'failure');
      assert.equal(requested.results.deploy, 'skipped');
    });
  }
}

test('skipped API tests allow frontend E2E and docs keep a successful check', () => {
  const web = selection(['apps/web/src/app/app.ts']).results;
  assert.equal(web.api, 'skipped');
  assert.equal(web.e2e, 'success');
  const docs = selection(['engineering/ci-jobs-2026-11-08.md']).results;
  for (const id of ['api', 'frontend', 'e2e', 'deploy']) assert.equal(docs[id], 'skipped');
  assert.equal(docs.check, 'success');
});

test('older successful main checks cannot queue a deployment', () => {
  assert.equal(
    selection(['apps/web/src/app/app.ts'], 'push', {}, 'false').results.deploy,
    'skipped',
  );
  assert.equal(selection(['apps/web/src/app/app.ts'], 'push').results.deploy, 'success');
});

test('only CI jobs cancel predecessors; production deployment is serialized without cancellation', () => {
  assert.equal(workflow.concurrency, undefined);
  for (const id of ['api', 'frontend', 'e2e']) {
    const group = workflow.jobs[id].concurrency;
    assert.equal(group['cancel-in-progress'], true);
    assert.ok(group.group.includes(id));
    assert.ok(group.group.includes('github.event_name'));
  }
  assert.equal(cd.jobs.deploy.concurrency['cancel-in-progress'], false);
  assert.equal(cd.jobs.deploy.concurrency.group, 'dopagaki-production');
  assert.deepEqual(workflow.jobs.e2e.needs, ['changes', 'api', 'frontend']);
});

test('the production image is built once in CI, tested, and handed to CD without rebuilding', () => {
  const steps = workflow.jobs.e2e.steps;
  assert.equal(
    steps.filter((step) => step.uses?.startsWith('docker/build-push-action@')).length,
    1,
  );
  const build = steps.find((step) => step.uses?.startsWith('docker/build-push-action@'));
  assert.equal(build.with['cache-from'], 'type=gha,scope=production');
  assert.ok(build.with['cache-to'].includes("github.event_name == 'push'"));
  assert.equal(build.env.DOCKER_BUILD_RECORD_UPLOAD, 'false');
  const browser = steps.find((step) => step.id === 'e2e');
  assert.equal(browser.env.E2E_SKIP_EXTENSION_BUILD, '1');
  assert.equal(browser.env.E2E_USE_BUILD, '1');
  assert.ok(browser.env.E2E_EXTERNAL_SERVER.includes('needs.changes.outputs.image'));
  const save = steps.findIndex((step) => step.id === 'handoff');
  assert.ok(save > steps.findIndex((step) => step.id === 'e2e'));
  assert.ok(steps[save].if.includes("github.event_name == 'push'"));
  assert.ok(
    !cd.jobs.deploy.steps.some((step) => step.uses?.startsWith('docker/build-push-action@')),
  );
  assert.ok(
    cd.jobs.deploy.steps.some((step) => step.uses?.startsWith('actions/download-artifact@')),
  );
  assert.equal(
    cd.jobs.deploy.steps.find((step) => step.name === 'Load and verify tested image').env
      .EXPECTED_IMAGE_ID,
    '${{ inputs.image-id }}',
  );
});

test('image readiness waits through a connection reset during startup', async (t) => {
  let requests = 0;
  const server = createServer((request, response) => {
    if (++requests === 1) request.socket.destroy();
    else response.end('{"status":"ok"}');
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  t.after(() => server.close());
  const startup = workflow.jobs.e2e.steps.find(
    (step) => step.name === 'Start tested production image',
  );
  const readiness = startup.run
    .split('\n')
    .find((line) => line.trimStart().startsWith('curl '))
    .replace('127.0.0.1:4300', `127.0.0.1:${server.address().port}`);
  const { stdout } = await promisify(execFile)('bash', ['-c', readiness], { timeout: 5000 });
  assert.equal(requests, 2);
  assert.equal(stdout, '{"status":"ok"}');
});

test('the demo listener crosses the container boundary while its host port stays on loopback', () => {
  const startup = workflow.jobs.e2e.steps.find(
    (step) => step.name === 'Start tested production image',
  );
  assert.match(startup.run, /-e APP_MODE=demo\b/);
  assert.match(startup.run, /-e DEMO_PUBLIC=true\b/);
  assert.match(startup.run, /-p 127\.0\.0\.1:4300:8080\b/);
});

test('CD preserves production permissions, same-SHA checkout and a fresh-main guard before side effects', () => {
  const steps = cd.jobs.deploy.steps;
  assert.equal(cd.jobs.deploy.environment.name, 'production');
  assert.equal(workflow.jobs.deploy.permissions['id-token'], 'write');
  assert.equal(steps[0].with['persist-credentials'], false);
  assert.equal(steps[0].with.ref, undefined);
  assert.equal(steps[1].run, 'bash deploy/current-main.sh');
  for (const step of steps.slice(2))
    assert.equal(step.if, "steps.current.outputs.current == 'true'");
  for (const id of ['changes', 'api', 'frontend', 'e2e', 'check'])
    assert.notEqual(workflow.jobs[id].permissions?.['id-token'], 'write');
});

test('existing Go and pnpm cache fallbacks remain usable', () => {
  for (const name of ['go', 'node']) {
    const action = load(
      readFileSync(new URL(`../.github/actions/setup-${name}/action.yml`, import.meta.url), 'utf8'),
    );
    const cache = action.runs.steps.find((step) => step.uses?.startsWith('actions/cache@'));
    assert.ok(cache.with['restore-keys'].length > 0);
  }
});

test('cold Go and Node setup use the official mise mirror without disabling caches', () => {
  for (const name of ['go', 'node']) {
    const action = load(
      readFileSync(new URL(`../.github/actions/setup-${name}/action.yml`, import.meta.url), 'utf8'),
    );
    const mise = action.runs.steps.find((step) => step.uses?.startsWith('jdx/mise-action@'));
    assert.equal(mise.with.fetch_from_github, false, name);
    assert.equal(mise.with.version, undefined, 'a pinned mise version bypasses the mirror');
    assert.equal(mise.with.cache, true, name);
    assert.ok(mise.with.install_args.includes(name === 'go' ? 'go' : 'node pnpm'));
  }
});

test('API lint uses the pinned official image and shares the existing caches as the runner user', (t) => {
  const dir = mkdtempSync(join(tmpdir(), 'dopagaki-lint-'));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  const action = load(
    readFileSync(new URL('../.github/actions/setup-go/action.yml', import.meta.url), 'utf8'),
  );
  assert.equal(action.runs.steps[0].with.install_args, 'go');
  const lint = workflow.jobs.api.steps.find((step) => step.name === 'API lint');
  assert.ok(lint, 'lint remains a required API step');
  const output = join(dir, 'docker-args');
  writeFileSync(
    join(dir, 'docker'),
    `#!${process.execPath}
import { writeFileSync } from 'node:fs';
writeFileSync(process.env.CALL_LOG, JSON.stringify(process.argv.slice(2)));
`,
    { mode: 0o755 },
  );
  const run = spawnSync('bash', ['-e', '-c', lint.run], {
    cwd: new URL('../apps/api/', import.meta.url),
    env: { ...process.env, PATH: `${dir}:${process.env.PATH}`, CALL_LOG: output },
    encoding: 'utf8',
  });
  assert.equal(run.status, 0, run.stderr);
  const args = JSON.parse(readFileSync(output, 'utf8'));
  const tools = readFileSync(new URL('../mise.toml', import.meta.url), 'utf8');
  const version = (name) => tools.match(new RegExp(`^${name} = "([^"]+)"`, 'm'))[1];
  assert.ok(args.includes(`golangci/golangci-lint:v${version('golangci-lint')}`));
  assert.ok(args.includes(`GOTOOLCHAIN=go${version('go')}`));
  for (const cache of ['go/pkg/mod', '.cache/go-build', '.cache/golangci-lint'])
    assert.ok(
      args.some((arg) => arg.startsWith(`${process.env.HOME}/${cache}:`)),
      cache,
    );
  assert.equal(args[args.indexOf('--user') + 1], `${process.getuid()}:${process.getgid()}`);
  assert.ok(workflow.jobs.api.steps.some((step) => step.run === 'go test -race ./...'));
});

test('smoke selection covers registration, signed-out privacy, extension work flow and accessibility', () => {
  for (const name of [
    'app.spec.ts',
    'login.spec.ts',
    'extension.spec.ts',
    'accessibility.spec.ts',
  ]) {
    assert.ok(
      readFileSync(new URL(`../e2e/tests/${name}`, import.meta.url), 'utf8').includes(
        "tag: '@smoke'",
      ),
      name,
    );
  }
  const specs = readdirSync(new URL('../e2e/tests/', import.meta.url)).filter((name) =>
    name.endsWith('.spec.ts'),
  );
  assert.ok(specs.length > 4);
});
