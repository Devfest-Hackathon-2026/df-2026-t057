import { appendFileSync } from 'node:fs';
import { pathToFileURL } from 'node:url';

const imageInputs = new Set([
  'Dockerfile',
  '.dockerignore',
  'package.json',
  'pnpm-lock.yaml',
  'pnpm-workspace.yaml',
  '.npmrc',
  'mise.toml',
  '.nvmrc',
  'tsconfig.base.json',
]);
const productionControls = new Set(['.github/workflows/cd.yml', 'deploy/cloud-run.sh']);
const ciRuntime = new Set([
  '.github/workflows/check.yml',
  '.github/ci-paths.yaml',
  'deploy/ci-policy.mjs',
  'deploy/current-main.sh',
  'deploy/save-image.sh',
  'deploy/load-image.sh',
]);

function testOrDocumentation(path) {
  return (
    /(?:\.test\.|\.spec\.|_test\.go$|\/testkit\/|\/testing\/|\/testdata\/)/.test(path) ||
    /(?:^|\/)(?:README(?:\.md)?|INSTALL\.txt|LICENSE[^/]*|vitest\.config\.ts|tsconfig\.spec\.json)$/.test(
      path,
    )
  );
}

function imageInput(path) {
  return (
    imageInputs.has(path) ||
    /^(?:apps|packages|e2e|deploy)\/.*package\.json$/.test(path) ||
    /^apps\/api\/go\.(?:mod|sum)$/.test(path)
  );
}

export function planChecks(files, flags, event = 'pull_request', requestDeploy = false) {
  const manual = event === 'workflow_dispatch';
  const redeploy = event === 'push' && requestDeploy;
  const result = Object.fromEntries(
    ['code', 'api', 'web', 'extension', 'shared', 'generate', 'e2e', 'deploy', 'terraform'].map(
      (key) => [key, flags[key] === true || flags[key] === 'true'],
    ),
  );
  const runtime = files.filter(
    (path) => !testOrDocumentation(path) && /^(?:apps|packages)\//.test(path),
  );
  const buildChanged = files.some(imageInput);
  const deploy =
    redeploy ||
    files.some((path) => productionControls.has(path)) ||
    buildChanged ||
    (result.deploy && runtime.some((path) => !path.startsWith('apps/extension/')));
  const ciChanged = files.some(
    (path) => ciRuntime.has(path) || path.startsWith('.github/actions/'),
  );
  const ciOnly =
    !manual &&
    !redeploy &&
    ciChanged &&
    !files.some((path) => productionControls.has(path)) &&
    files.every(
      (path) =>
        ciRuntime.has(path) ||
        path.startsWith('.github/') ||
        (path.startsWith('deploy/') && testOrDocumentation(path)) ||
        path.endsWith('.md'),
    );
  const validateImage =
    buildChanged ||
    files.some(
      (path) =>
        ciRuntime.has(path) ||
        path === '.github/workflows/cd.yml' ||
        path === 'e2e/playwright.config.ts',
    );
  const docker = event === 'pull_request' && validateImage && !ciOnly;
  const browser =
    !ciOnly &&
    (manual ||
      runtime.length > 0 ||
      buildChanged ||
      (event === 'pull_request' && (ciChanged || docker)) ||
      files.some((path) => path.startsWith('e2e/')) ||
      (event === 'push' && deploy));
  const suite = !browser ? 'none' : manual ? 'full' : 'smoke';
  const generate = files.some(
    (path) =>
      [
        'package.json',
        'pnpm-lock.yaml',
        'pnpm-workspace.yaml',
        'mise.toml',
        '.npmrc',
        '.nvmrc',
        '.prettierrc.json',
        '.prettierignore',
      ].includes(path) ||
      /^apps\/api\/go\.(?:mod|sum)$/.test(path) ||
      (['packages/contract/', 'packages/tokens/', 'apps/api/internal/adapter/http/oapi/'].some(
        (prefix) => path.startsWith(prefix),
      ) &&
        !testOrDocumentation(path)),
  );
  if (manual || redeploy) {
    for (const key of ['code', 'api', 'web', 'extension', 'shared', 'terraform'])
      result[key] = true;
  }
  if (ciOnly) {
    for (const key of ['api', 'web', 'extension', 'shared', 'terraform']) result[key] = false;
  }
  return {
    ...result,
    generate: manual || redeploy || generate,
    deploy,
    e2e: browser,
    suite,
    image: (event === 'push' && deploy) || docker,
    docker,
  };
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const changes = JSON.parse(process.env.CHANGES);
  const plan = planChecks(
    JSON.parse(changes.all_files),
    changes,
    process.env.EVENT_NAME,
    process.env.REQUEST_DEPLOY === 'true',
  );
  appendFileSync(
    process.env.GITHUB_OUTPUT,
    Object.entries(plan)
      .filter(([key]) => !key.endsWith('_files') && !key.endsWith('_count'))
      .map(([key, value]) => `${key}=${value}\n`)
      .join(''),
  );
}
