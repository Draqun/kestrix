import { defineConfig, mergeConfig } from 'vitest/config';
import baseConfig from './vitest.config.js';

export default mergeConfig(baseConfig, defineConfig({
  test: {
    isolate: true,
    coverage: {
      enabled: true,
      provider: 'v8',
      reporter: ['text', 'json-summary', 'html'],
      reportsDirectory: './coverage/frontend',
      include: [
        'src/components/Layout.jsx',
        'src/components/accounts/*.jsx',
        'src/components/credentials/*.jsx',
        'src/hooks/useAccounts.js',
        'src/hooks/useCredentials.js',
        'src/hooks/useKeyboardNav.js',
        'src/lib/credentialForm.js',
        'src/lib/projectForm.js',
        'src/pages/Settings.jsx',
      ],
      thresholds: {
        statements: 94,
        branches: 89,
        functions: 93,
        lines: 95,
      },
    },
  },
}));
