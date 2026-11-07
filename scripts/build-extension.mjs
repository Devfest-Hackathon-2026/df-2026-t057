import { build } from 'esbuild';
import { cp, mkdir, readFile, rm, writeFile } from 'node:fs/promises';

const origin = new URL(process.env.APP_URL ?? 'http://127.0.0.1:4200').origin;
const version = process.env.RELEASE_TAG
  ? process.env.RELEASE_TAG.replace(/^v/, '')
  : JSON.parse(await readFile('package.json', 'utf8')).version;
if (
  !/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(version) ||
  version.split('.').some((part) => Number(part) > 65535) ||
  version === '0.0.0'
) {
  throw new Error(
    'Extension version must be X.Y.Z, with each number between 0 and 65535 (not all zero).',
  );
}
const outdir = 'dist/extension';
await rm(outdir, { recursive: true, force: true });
await mkdir(outdir, { recursive: true });
await cp('extension/public', outdir, { recursive: true });
await cp('web/public/gold-light.png', `${outdir}/gold-light.png`);
await build({
  entryPoints: [
    'extension/src/background.ts',
    'extension/src/content.ts',
    'extension/src/popup.ts',
  ],
  outdir,
  bundle: true,
  format: 'iife',
  target: 'chrome120',
  minify: true,
  plugins: [
    {
      name: 'management-origin',
      setup(build) {
        build.onLoad({ filter: /protocol\.ts$/ }, async (args) => ({
          contents: (await readFile(args.path, 'utf8')).replaceAll('__MANAGEMENT_ORIGIN__', origin),
          loader: 'ts',
        }));
      },
    },
  ],
});
await writeFile(
  `${outdir}/manifest.json`,
  JSON.stringify(
    {
      manifest_version: 3,
      name: 'DOPAGAKI',
      version,
      description: '課題を終えて、次の楽しみに。カレンダー連携と作業タイマー。',
      minimum_chrome_version: '120',
      permissions: ['storage', 'alarms', 'notifications', 'scripting', 'activeTab'],
      host_permissions: [`${origin}/*`, 'https://www.youtube.com/*'],
      optional_host_permissions: ['https://*/*', 'http://*/*'],
      background: { service_worker: 'background.js' },
      action: { default_popup: 'popup.html', default_title: 'DOPAGAKI' },
      icons: { 16: 'icon-16.png', 48: 'icon-48.png', 128: 'icon-128.png' },
      content_scripts: [
        {
          matches: [`${origin}/*`, 'https://www.youtube.com/*'],
          js: ['content.js'],
          run_at: 'document_start',
        },
      ],
      web_accessible_resources: [
        { resources: ['content.css', 'gold-light.png'], matches: ['<all_urls>'] },
      ],
      content_security_policy: { extension_pages: "script-src 'self'; object-src 'self'" },
    },
    null,
    2,
  ) + '\n',
);
console.log(`Extension built: ${outdir} (${origin})`);
