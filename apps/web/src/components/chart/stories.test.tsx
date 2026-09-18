import { render } from '@testing-library/react'
import type * as React from 'react'
import { composeStories } from '@storybook/react'
import { describe, expect, it } from 'vitest'

import { LocaleProvider } from '@/lib/i18n/context'

import * as planetTable from './PlanetTable.stories'
import * as sadeSati from './SadeSatiIndicator.stories'
import * as yogaCard from './YogaCard.stories'

/**
 * Every story renders.
 *
 * ── Why a build is not enough ──
 *
 * `storybook build` compiles the stories; it does not execute them. A
 * story whose args no longer match its component's props type-checks
 * only until the props change, and then renders a crash that nobody
 * sees until they open that panel by hand.
 *
 * These are exactly the states nobody opens by hand — that is the whole
 * reason they were written down. "Sade Sati in the rising phase" is
 * visible in the running app for about thirty months every twenty-nine
 * years. A story for it that throws is indistinguishable from one that
 * works, until somebody needs it.
 *
 * ── What this does NOT check ──
 *
 * Appearance. jsdom has no layout, so this cannot tell whether a card
 * overlaps its neighbour or a colour is unreadable; the visual
 * regression suite in `tests/e2e/visual.spec.ts` covers that. This
 * checks the cheaper and more brittle half: that the component and its
 * args still agree.
 */

const SUITES = {
  PlanetTable: planetTable,
  SadeSatiIndicator: sadeSati,
  YogaCard: yogaCard,
} as const

describe('every Storybook story renders', () => {
  for (const [component, module] of Object.entries(SUITES)) {
    const stories = composeStories(module)
    const names = Object.keys(stories)

    it(`${component} has stories at all`, () => {
      // A module that exported nothing would make every assertion below
      // vacuous — the loop would simply not run, and the suite would
      // report a pass for a component with no coverage.
      expect(names.length, `${component}.stories.tsx exports no stories`).toBeGreaterThan(0)
    })

    for (const name of names) {
      it(`${component}/${name}`, () => {
        /*
          Typed as a component rather than left to inference.

          `composeStories` over a heterogeneous record of modules widens
          the story type to a union, and TypeScript then refuses it in
          JSX position — "does not have any construct or call
          signatures". The cast says what is true: each composed story
          IS a renderable component, and the modules differ only in
          which props they close over.
        */
        const Story = stories[name as keyof typeof stories] as React.ComponentType

        // The provider is applied by .storybook/preview.tsx in the real
        // Storybook. composeStories does not run the preview decorators
        // here, so it is applied explicitly — without it every story
        // throws "useLocale must be used inside a LocaleProvider", which
        // would be this test failing on its own setup rather than on
        // anything about the component.
        const { container } = render(
          <LocaleProvider>
            <Story />
          </LocaleProvider>,
        )

        expect(
          container.textContent?.trim().length ?? 0,
          `${component}/${name} rendered nothing at all`,
        ).toBeGreaterThan(0)
      })
    }
  }
})
