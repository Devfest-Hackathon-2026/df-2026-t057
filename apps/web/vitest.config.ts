import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    // Material の部品を初めて描画するテストは重く、CI など負荷の高い環境では5秒を超えることがある。
    testTimeout: 15_000,
  },
});
