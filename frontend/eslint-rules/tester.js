import { RuleTester } from 'eslint';
import tseslintParser from '@typescript-eslint/parser';

// Shared RuleTester setup: the project's TS parser with JSX on, as in
// eslint.config.js. RuleTester picks up vitest's global describe/it.
export const ruleTester = new RuleTester({
  languageOptions: {
    parser: tseslintParser,
    parserOptions: { ecmaFeatures: { jsx: true }, sourceType: 'module', ecmaVersion: 2021 },
  },
});
