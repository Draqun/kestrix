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
  },
});
