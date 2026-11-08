import { defineConfig } from 'vitest/config';

export default defineConfig({
  define: { __MANAGEMENT_ORIGIN__: JSON.stringify('http://127.0.0.1:4200') },
  test: {
    include: ['**/*.test.ts'],
    exclude: ['node_modules/**', '.output/**', '.wxt/**'],
    environment: 'node',
  },
});
