import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';
import { test } from 'node:test';
import { runInNewContext } from 'node:vm';
import { load } from 'js-yaml';
import picomatch from 'picomatch';

const workflow = load(
  readFileSync(new URL('../.github/workflows/check.yml', import.meta.url), 'utf8'),
);
const filterStep = workflow.jobs.check.steps.find((step) => step.id === 'changes');
const filters = load(
  filterStep.with.filters.endsWith('.yaml')
    ? readFileSync(new URL(`../${filterStep.with.filters}`, import.meta.url), 'utf8')
    : filterStep.with.filters,
);

function selection(files, event = 'pull_request') {
  const outputs = Object.fromEntries(
    Object.entries(filters).map(([name, patterns]) => [
      name,
      String(files.some(picomatch(patterns.flat(Infinity), { dot: true }))),
    ]),
  );
  const context = {
    github: { event_name: event },
    steps: { changes: { outputs }, e2e: { outcome: 'success' } },
    failure: () => false,
  };
  const steps = workflow.jobs.check.steps.filter((step) => {
    if (!step.if) return true;
    return runInNewContext(step.if, context);
  });
  const expression = workflow.jobs.check.steps.find((step) => step.id === 'e2e').env.EXTENSION_ONLY;
  const extensionOnly = runInNewContext(expression.slice(3, -2), context);
  return {
    outputs,
    steps,
    extensionOnly,
    commands: steps.map((step) => step.run ?? '').join('\n'),
  };
}

test('extension PR skips Go checks, web unit tests and code generation', () => {
  const { commands, extensionOnly } = selection(['apps/extension/background/connection.ts']);
  assert.equal(extensionOnly, true);
  assert.doesNotMatch(commands, /pnpm (check|test|generate)\s*$/m);
  assert.doesNotMatch(commands, /@dopagaki\/api (lint|test)/);
  assert.doesNotMatch(commands, /@dopagaki\/web test/);
  assert.match(commands, /@dopagaki\/extension test/);
  assert.match(commands, /test:e2e/);
});

test('API PR keeps race tests and full E2E without frontend unit tests', () => {
  const { commands, extensionOnly } = selection(['apps/api/internal/wallet/domain.go']);
  assert.equal(extensionOnly, false);
  assert.match(commands, /@dopagaki\/api test/);
  assert.match(commands, /test:e2e/);
  assert.doesNotMatch(commands, /@dopagaki\/(web|extension) test/);
});

test('web PR keeps browser integration tests without Go unit tests', () => {
  const { commands, extensionOnly } = selection(['apps/web/src/app/app.ts']);
  assert.equal(extensionOnly, false);
  assert.match(commands, /@dopagaki\/web test/);
  assert.match(commands, /test:e2e/);
  assert.doesNotMatch(commands, /@dopagaki\/api (lint|test)/);
});

for (const shared of ['bridge', 'contract', 'tokens', 'messages']) {
  test(`${shared} changes check both frontend consumers and the production container`, () => {
    const { outputs, commands, steps } = selection([`packages/${shared}/src/index.ts`]);
    assert.equal(outputs.web, 'true');
    assert.equal(outputs.extension, 'true');
    assert.equal(outputs.deploy, 'true');
    assert.match(commands, /@dopagaki\/web test/);
    assert.match(commands, /@dopagaki\/extension test/);
    assert.ok(steps.some((step) => step.name === 'Validate production container'));
  });
}

test('main retains all checks after an extension change', () => {
  const { commands, extensionOnly } = selection(
    ['apps/extension/background/connection.ts'],
    'push',
  );
  assert.equal(extensionOnly, false);
  for (const command of [
    'pnpm generate',
    '@dopagaki/api lint',
    '@dopagaki/api test',
    '@dopagaki/web test',
    '@dopagaki/extension test',
    'test:e2e',
  ]) {
    assert.ok(commands.includes(command), command);
  }
});

test('changing E2E support together with the extension runs the complete browser suite', () => {
  assert.equal(
    selection(['apps/extension/entrypoints/background.ts', 'e2e/support/api.ts']).extensionOnly,
    false,
  );
});

test('the extension-only pattern includes every current extension integration spec', () => {
  const directory = new URL('../e2e/tests/', import.meta.url);
  for (const name of readdirSync(directory).filter((name) => name.endsWith('.spec.ts'))) {
    if (readFileSync(new URL(name, directory), 'utf8').includes("from '../support/extension'")) {
      assert.match(name, /extension.*\.spec\.ts/, name);
    }
  }
});

test('documentation-only changes do not install tools or run checks', () => {
  const { steps } = selection(['README.md', 'engineering/ci-savings.md']);
  assert.deepEqual(
    steps.map((step) => step.uses),
    ['actions/checkout@v7', 'dorny/paths-filter@v4'],
  );
});

test('deploy-only PR runs deploy regression tests and validates the container', () => {
  const { commands, steps } = selection(['deploy/cloud-run.sh']);
  assert.match(commands, /@dopagaki\/deploy test/);
  assert.doesNotMatch(commands, /test:e2e/);
  assert.ok(steps.some((step) => step.name === 'Validate production container'));
});

for (const path of [
  '.npmrc',
  'tsconfig.base.json',
  '.github/ci-paths.yaml',
  '.github/workflows/check.yml',
  'pnpm-lock.yaml',
]) {
  test(`${path} invalidates all relevant checks`, () => {
    const { outputs } = selection([path]);
    for (const flag of ['code', 'api', 'web', 'extension', 'shared', 'generate', 'e2e']) {
      assert.equal(outputs[flag], 'true', flag);
    }
  });
}

test('container verification and deployment share cache settings without uploading build artifacts', () => {
  for (const job of Object.values(workflow.jobs)) {
    const build = job.steps.find(
      (step) =>
        step.name === 'Validate production container' || step.name === 'Build production image',
    );
    assert.match(build.uses ?? '', /^docker\/build-push-action@/);
    assert.equal(build.with['cache-from'], 'type=gha,scope=production');
    assert.equal(build.with['cache-to'], 'type=gha,mode=max,scope=production');
    assert.equal(build.env.DOCKER_BUILD_RECORD_UPLOAD, 'false');
  }
});
