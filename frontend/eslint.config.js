import tseslintPlugin from '@typescript-eslint/eslint-plugin';
import tseslintParser from '@typescript-eslint/parser';
import reactHooksPlugin from 'eslint-plugin-react-hooks';
import helmPlugin from './eslint-rules/index.js';

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
      helm: helmPlugin,
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
      // AGENTS.md "Tiles And Widgets": the operator-visible word is "tile";
      // "widget" must not appear in JSX text, in aria-label, title,
      // placeholder, alt, label, description and similar attributes, or in
      // toast messages. Text the server returns is not covered.
      'helm/no-widget-in-ui-text': 'error',
      // ADR 0142: a CRUD surface (an index or a details page) is built from
      // components/patterns (Page/IndexTable/IndexFilters/...), not from
      // ui/table directly - that's exactly the hand-rolled-per-page drift
      // the pattern library exists to close. The override below lifts this
      // for components/patterns itself (IndexTable is the one legitimate
      // caller) and for the surfaces this cycle didn't migrate.
      'no-restricted-imports': [
        'error',
        {
          paths: [
            {
              name: '@/components/ui/table',
              message: 'Build CRUD tables from components/patterns/index-table (IndexTable) instead of ui/table directly - see docs/adr/0142-crud-pattern-library.md.',
            },
          ],
        },
      ],
    },
  },
  {
    // The class-token rules read string literals anywhere in a file, so test
    // fixtures that spell out a bad class on purpose are left alone.
    files: ['src/**/*.{ts,tsx}'],
    ignores: ['**/*.test.*', 'src/test/**', '**/__tests__/**'],
    rules: {
      // AGENTS.md "Micro-Typography Scale": a text size (text-[Npx],
      // text-[Nrem], JSX fontSize, fontSize properties) is allowed only at
      // 9px, 10px or 12px and above (0 hides text); text-[11px] is retired.
      'helm/text-size-scale': 'error',
      // AGENTS.md "Anti-Slop Constraints": no hex, rgb() or hsl() colour
      // escapes in class tokens, SVG fill/stroke/color/stopColor or style
      // colour properties; use a semantic token.
      'helm/no-arbitrary-color': 'error',
      // ADR 0228: every time the operator reads is in the boat's zone, so no
      // device-zone formatting or local getters. A deliberate device-clock
      // reading opts out with "// helm-browser-zone: <reason>".
      'helm/no-zoneless-time': 'error',
    },
  },
  {
    // Pending migration to components/patterns (ADR 0142's own migration
    // order: Equipment, Locations and Documents done; Profiles, Maintenance,
    // Wall displays remain). Remove an entry here as each
    // surface migrates, rather than adding a compatibility exception that
    // outlives the migration it was for.
    files: [
      'src/components/patterns/**/*.{ts,tsx}',
      'src/components/display-editor-panel.tsx',
      'src/components/inventory/maintenance-section.tsx',
      'src/components/wall-displays-panel.tsx',
    ],
    rules: {
      'no-restricted-imports': 'off',
    },
  },
];
