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
    // Why 60s and not some other number: the slowest test here is now
    // CredentialSelectField's inline-creation flow at ~2.3s locally, not the
    // 838ms this comment assumed when it justified 20s. Measured against that
    // red run, the runner ran individual tests 5.7-9.2x slower than local
    // (21040ms vs 2285ms for that same test, which is exactly how it blew
    // through the 20s ceiling by a second). Worst case therefore lands around
    // 21s; 60s keeps roughly 2.8x headroom over it. Re-derive this number if a
    // test lands that is materially slower than ~2.3s locally.
    testTimeout: 60000,
    hookTimeout: 60000,
  },
});
