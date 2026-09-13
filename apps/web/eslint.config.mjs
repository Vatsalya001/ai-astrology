import js from '@eslint/js'
import tseslint from 'typescript-eslint'
import jsxA11y from 'eslint-plugin-jsx-a11y'
import next from 'eslint-config-next/core-web-vitals'

/**
 * ESLint for the web app.
 *
 * Go has golangci-lint and Python has ruff; TypeScript had nothing until
 * this file — `task lint:web` re-ran `tsc`, which is a type checker, not
 * a linter. The two catch disjoint classes of bug: `tsc` is perfectly
 * happy with a floating promise, an unused variable or an `<img>` with
 * no alt text.
 *
 * `next lint` is not an option: Next 16 removed it. eslint-config-next
 * 16 exports native flat config, so no `FlatCompat` shim is needed.
 */
export default tseslint.config(
  {
    ignores: ['.next/**', 'node_modules/**', 'next-env.d.ts'],
  },

  js.configs.recommended,
  ...tseslint.configs.recommended,
  ...next,

  {
    rules: {
      // Accessibility is in the Definition of Done, so it is machine-
      // checked rather than left to review. next/core-web-vitals enables
      // a subset; `strict` adds label association and interactive-element
      // semantics, which is where this app's real risk sits — forms in
      // Phase 1, the chart SVG in Phase 3.
      //
      // Only the rules are spread, not the whole config:
      // eslint-config-next already registers the jsx-a11y plugin, and
      // flat config refuses to let a plugin name be defined twice.
      ...jsxA11y.flatConfigs.strict.rules,

      // Unused code is how a half-finished refactor ships. The underscore
      // prefix is the documented escape hatch for genuinely-unused rest
      // siblings and required-but-ignored parameters.
      '@typescript-eslint/no-unused-vars': [
        'error',
        {
          argsIgnorePattern: '^_',
          varsIgnorePattern: '^_',
          caughtErrorsIgnorePattern: '^_',
        },
      ],
      // `any` defeats the point of `strict` in tsconfig.
      '@typescript-eslint/no-explicit-any': 'error',
    },
  },
)
