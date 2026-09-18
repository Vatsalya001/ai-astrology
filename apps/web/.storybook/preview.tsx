import type { Decorator, Preview } from '@storybook/nextjs-vite'
import * as React from 'react'

import { LocaleProvider } from '../src/lib/i18n/context'

import '../src/styles/globals.css'

/**
 * Every story renders inside the app's real providers and on the app's
 * real background.
 *
 * ── Why the provider is not optional ──
 *
 * Most components here call `useLocale()`, which throws outside a
 * `LocaleProvider` — so a story without it does not render a broken
 * component, it renders a crash. Wrapping globally means a new story is
 * one export rather than five lines of setup, which is the difference
 * between stories that get written and stories that do not.
 *
 * ── Why the dark background is not decoration ──
 *
 * The palette is midnight navy with near-white text. On Storybook's
 * default white canvas, `text-ink` (#F2F3F8) is invisible — so a
 * component would look broken while being correct, and the one after it
 * would look fine while being wrong. `bg-base` makes the canvas the same
 * surface the app uses.
 */
const withProviders: Decorator = (Story) => (
  <LocaleProvider>
    <div className="bg-base text-ink min-h-[8rem] p-6">
      <Story />
    </div>
  </LocaleProvider>
)

/**
 * A direction switch, for layout robustness rather than for a locale.
 *
 * The spec asks for an RTL variant. This product ships English and
 * Hindi, and neither is right-to-left — so there is no RTL locale to
 * screenshot, and claiming one would be counting a box nobody can tick.
 *
 * What the toggle IS good for: it surfaces hardcoded `left`/`ml-` and
 * absolute positioning that would break the day an Urdu or Arabic locale
 * is added. Cheap to keep, honest about what it proves.
 */
const withDirection: Decorator = (Story, context) => {
  const dir = context.globals.direction === 'rtl' ? 'rtl' : 'ltr'
  return (
    <div dir={dir}>
      <Story />
    </div>
  )
}

const preview: Preview = {
  decorators: [withDirection, withProviders],

  globalTypes: {
    direction: {
      description: 'Text direction — for layout robustness, not a shipped locale',
      defaultValue: 'ltr',
      toolbar: {
        title: 'Direction',
        icon: 'transfer',
        items: [
          { value: 'ltr', title: 'LTR (en, hi)' },
          { value: 'rtl', title: 'RTL (layout check only)' },
        ],
      },
    },
  },

  parameters: {
    backgrounds: { disable: true }, // the decorator owns the background
    layout: 'fullscreen',
    a11y: {
      // Report violations rather than failing the story outright: a
      // component rendered in isolation legitimately lacks page
      // landmarks that the real screen provides.
      test: 'todo',
    },
  },
}

export default preview
