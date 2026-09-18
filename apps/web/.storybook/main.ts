import type { StorybookConfig } from '@storybook/nextjs-vite'

/**
 * Storybook for the component library.
 *
 * Task 3.17. The spec's reason is worth restating, because it is not
 * "documentation": *"Storybook is free and it is what keeps Phases 5 and
 * 10 from re-inventing all of this."* Phase 5 builds a chat UI on these
 * components and Phase 10 ports them to React Native. Both need to see
 * what exists without reading twenty files.
 *
 * ── Why the states matter more than the gallery ──
 *
 * The Definition of Done asks for loading, error and empty variants on
 * every component. Those are the states you cannot easily reach in the
 * running app — the empty state needs an account with no birth profile,
 * the error state needs the API to fail — and they are consequently the
 * ones that rot. A story is the cheapest way to look at them.
 */
const config: StorybookConfig = {
  stories: ['../src/**/*.stories.@(ts|tsx)'],

  addons: [
    // Runs axe on every story, in the panel. Not a replacement for the
    // e2e a11y specs — those check whole pages in a real browser — but
    // it catches a missing label at the moment the component is being
    // worked on rather than at the end of the phase.
    '@storybook/addon-a11y',
    '@storybook/addon-docs',
  ],

  framework: {
    name: '@storybook/nextjs-vite',
    options: {},
  },

  // No telemetry. This is a private product and its component names are
  // not something to send anywhere by default.
  core: { disableTelemetry: true },

  // No staticDirs. This app has no public/ directory — its icons are
  // imported as modules and next/font handles the typefaces, so there
  // is nothing to serve statically. Naming one that does not exist
  // fails the build outright.
}

export default config
