import { readFileSync } from 'node:fs';
import { defineConfig } from 'wxt';
import { EXTENSION_ID } from './buildconfig/extension-key';
import { managementMatches, manifestFor } from './buildconfig/manifest';
import { resolveManagementOrigin, resolveVersion } from './buildconfig/release';

const packageJson = JSON.parse(
  readFileSync(new URL('./package.json', import.meta.url), 'utf8'),
) as {
  version: string;
};
const origin = resolveManagementOrigin(process.env['APP_URL']);
const version = resolveVersion(process.env['RELEASE_TAG'], packageJson.version);

export default defineConfig({
  // 明示的に import する（どこから来た関数かをコードで読めるようにする）。
  imports: false,
  manifestVersion: 3,
  browser: 'chrome',
  targetBrowsers: ['chrome'],
  manifest: manifestFor(origin, version),
  zip: {
    artifactTemplate: 'dopagaki-extension-{{version}}-{{browser}}.zip',
  },
  vite: () => ({
    define: { __MANAGEMENT_ORIGIN__: JSON.stringify(origin) },
    build: { target: 'chrome120' },
  }),
  hooks: {
    // 配布するときに API の EXTENSION_IDS へ書く値なので、ビルドのたびに表示する。
    'build:done': (wxt) => {
      wxt.logger.info(`Extension ID: ${EXTENSION_ID}`);
    },
    // 管理画面の content script は、ビルド時に決まるオリジンでだけ動かす。
    'entrypoints:resolved': (_wxt, entrypoints) => {
      for (const entrypoint of entrypoints) {
        if (entrypoint.type === 'content-script' && entrypoint.name === 'management') {
          entrypoint.options.matches = managementMatches(origin);
        }
      }
    },
  },
});
