import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { spawnSync } from 'node:child_process';
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
  repository = 'a-sumikawa/devfest-2026',
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
    github: {
      repository,
      event_name: event,
      ref: event === 'push' ? 'refs/heads/main' : 'refs/pull/1/merge',
    },
    needs,
    steps: { e2e: { outcome: 'success' } },
    failure: () => false,
    cancelled: () => false,
    always: () => true,
  };
  const selected = [];
  // GitHubの暗黙のsuccess()は、直接のneedsだけでなく祖先ジョブのskipも見る。
  function ancestors(job) {
    return [job.needs]
      .flat()
      .filter(Boolean)
      .flatMap((name) => [name, ...ancestors(workflow.jobs[name])]);
  }
  for (const [id, job] of Object.entries(workflow.jobs)) {
    if (id === 'changes') continue;
    const implicitSuccess =
      /(?:always|cancelled|failure|success)\(/.test(job.if ?? '') ||
      ancestors(job).every((name) => needs[name].result === 'success');
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

// 主要な選択と配信境界を検査する。設定の文字列・順序をなぞる検査は持たない。
test('通常CIはPR/mainとも2本のスモーク、全件は手動だけ', () => {
  for (const event of ['pull_request', 'push']) {
    for (const path of [
      'apps/web/src/app/app.ts',
      'apps/api/internal/wallet/domain.go',
      'e2e/tests/login.spec.ts',
    ]) {
      assert.equal(selection([path], event).plan.suite, 'smoke');
    }
  }
  const manual = selection([], 'workflow_dispatch');
  assert.equal(manual.plan.suite, 'full');
  assert.equal(manual.results.deploy, 'skipped');
  assert.equal(workflow.on.schedule, undefined);
});

test('資料・単体テスト・CI専用変更はブラウザーも配信も起動しない', () => {
  for (const paths of [
    ['README.md'],
    ['apps/api/internal/wallet/domain_test.go'],
    ['.github/workflows/check.yml', 'deploy/ci-policy.mjs'],
  ]) {
    for (const event of ['pull_request', 'push']) {
      const { plan, results } = selection(paths, event);
      assert.equal(plan.suite, 'none');
      assert.equal(results.deploy, 'skipped');
    }
  }
});

test('認証・報酬の単体テストと契約生成の検査を残す', () => {
  assert.equal(selection(['apps/api/internal/wallet/domain.go']).results.api, 'success');
  assert.equal(selection(['apps/web/src/app/core/auth/auth.service.ts']).plan.web, true);
  assert.equal(selection(['packages/contract/openapi.yaml']).plan.generate, true);
});

test('失敗・取消・古いmainから配信しない', () => {
  for (const job of ['changes', 'api', 'frontend', 'e2e']) {
    for (const result of ['failure', 'cancelled']) {
      const { results } = selection(['apps/api/internal/wallet/domain.go'], 'push', {
        [job]: result,
      });
      assert.equal(results.check, 'failure');
      assert.equal(results.deploy, 'skipped');
    }
  }
  assert.equal(
    selection(['apps/web/src/app/app.ts'], 'push', {}, 'false').results.deploy,
    'skipped',
  );
});

test('配信対象はmainだけで、検査済みイメージを引き渡す', () => {
  const paths = ['apps/web/src/app/app.ts'];
  assert.equal(selection(paths).results.deploy, 'skipped');
  const main = selection(paths, 'push');
  assert.equal(main.results.deploy, 'success');
  assert.equal(main.plan.image, true);
  const steps = workflow.jobs.e2e.steps;
  assert.ok(steps.findIndex((s) => s.id === 'handoff') > steps.findIndex((s) => s.id === 'e2e'));
  assert.equal(cd.jobs.deploy.concurrency['cancel-in-progress'], false);
});

test('対象外の検査がskipでも合格済みの最新mainは配信する', () => {
  for (const path of ['apps/api/internal/viewing/service.go', 'apps/web/src/app/app.ts']) {
    const { results } = selection([path], 'push');
    assert.equal(results.terraform, 'skipped');
    assert.equal(results.check, 'success');
    assert.equal(results.deploy, 'success');
  }
});

test('提出用・別リポジトリからは検査に成功しても本番配信しない', () => {
  for (const repository of ['Devfest-Hackathon-2026/df-2026-t057', 'other/fork']) {
    const { results } = selection(
      ['apps/web/src/app/app.ts'],
      'push',
      {},
      'true',
      false,
      repository,
    );
    assert.equal(results.check, 'success');
    assert.equal(results.deploy, 'skipped');
    assert.equal(
      runInNewContext(cd.jobs.deploy.if, {
        github: { repository, event_name: 'push', ref: 'refs/heads/main' },
      }),
      false,
    );
  }
});
