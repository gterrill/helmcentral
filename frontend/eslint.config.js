import tseslintPlugin from '@typescript-eslint/eslint-plugin';
import tseslintParser from '@typescript-eslint/parser';
import reactHooksPlugin from 'eslint-plugin-react-hooks';

export default [
  {
    ignores: [
      'dist',
      'node_modules',
      'ds-bundle',
      '.migration',
      '.design-sync',
      '.ds-sync',
      'wind-compass-mockup',
    ],
  },
  {
    files: ['src/**/*.{ts,tsx}'],
    languageOptions: {
      parser: tseslintParser,
      parserOptions: {
        ecmaFeatures: { jsx: true },
        sourceType: 'module',
        ecmaVersion: 2021,
      },
      globals: {
        window: 'readonly',
        document: 'readonly',
        navigator: 'readonly',
        console: 'readonly',
        fetch: 'readonly',
        localStorage: 'readonly',
        sessionStorage: 'readonly',
        setTimeout: 'readonly',
        clearTimeout: 'readonly',
        setInterval: 'readonly',
        clearInterval: 'readonly',
      },
    },
    plugins: {
      '@typescript-eslint': tseslintPlugin,
      'react-hooks': reactHooksPlugin,
    },
    rules: {
      ...tseslintPlugin.configs.recommended.rules,
      // react-hooks' "recommended" config also bundles the React Compiler
      // rule family (refs, set-state-in-effect, purity, immutability, etc.)
      // - those flag plenty of idiomatic code that's only a problem if this
      // project adopts the compiler. Enable just the two classic hook-safety
      // rules until that's actually on the roadmap.
      'react-hooks/rules-of-hooks': 'error',
      'react-hooks/exhaustive-deps': 'warn',
    },
  },
];
