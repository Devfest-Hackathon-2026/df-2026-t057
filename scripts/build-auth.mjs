import { build } from 'esbuild';
await build({
  entryPoints: ['web/auth/extension-auth.ts', 'web/auth/deletion-status.ts'],
  outdir: 'dist/auth',
  bundle: true,
  format: 'iife',
  target: 'chrome120',
  minify: true,
});
