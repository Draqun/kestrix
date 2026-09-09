import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    include: ['src/**/*.component.test.jsx'],
    setupFiles: ['./src/test/setup.js'],
    clearMocks: true,
    pool: 'threads',
    fileParallelism: false,
    maxWorkers: 1,
    isolate: true,
    // One worker, no file parallelism and isolation per file means every one
    // of these files imports the whole MUI tree on its own: ~1.3s per file
    // locally, but over 20x that on a loaded CI runner (a red test_fe once
    // reported 573s of import time against 39s locally). The 5s default then
    // expires inside userEvent.type() in tests that have nothing to do with
    // the change under test. These timeouts bound a starved worker, not a
    // hanging test - a genuinely stuck test still fails, just later.
    //
    // Why 20s and not some other number: the slowest test here takes 838ms
    // locally, and the runner's execution phase came out 12-16x slower than
    // local in the red run, so the worst case lands around 10-13.5s. Twenty
    // leaves roughly 1.5-2x headroom over that; the 5s default sat below it.
    testTimeout: 20000,
    hookTimeout: 20000,
  },
});
