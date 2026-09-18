import type { Meta, StoryObj } from '@storybook/nextjs-vite'

import { YogaCard } from './YogaCard'

/**
 * A yoga, with its description.
 *
 * The strength word is the thing to watch. The spec forbids a score, a
 * percentage or a star rating — those invite ranking a person's chart —
 * so strength is a word, and the three renderings need to be
 * distinguishable without colour.
 */
const meta = {
  title: 'Chart/YogaCard',
  component: YogaCard,
} satisfies Meta<typeof YogaCard>

export default meta
type Story = StoryObj<typeof meta>

export const Strong: Story = {
  args: {
    yoga: {
      name: 'gaja_kesari',
      strength: 'strong',
      involvedPlanets: ['Jupiter', 'Moon'],
      involvedHouses: [1, 4],
    },
  },
}

export const Moderate: Story = {
  args: {
    yoga: {
      name: 'budhaditya',
      strength: 'moderate',
      involvedPlanets: ['Sun', 'Mercury'],
      involvedHouses: [10],
    },
  },
}

/**
 * A strength the UI does not know.
 *
 * astro-service documents two values, `strong` and `moderate`, and the
 * type is `string` rather than a union because the engine owns that
 * vocabulary. If a third ever arrives, the card must render it rather
 * than fall through to a blank — this story is what that looks like.
 */
export const UnknownStrength: Story = {
  args: {
    yoga: {
      name: 'chandra_mangala',
      strength: 'weak',
      involvedPlanets: ['Moon', 'Mars'],
      involvedHouses: [7],
    },
  },
}

/**
 * A yoga the corpus has no entry for.
 *
 * The engine can find a yoga that `@ayana/content` does not describe —
 * the corpus is scoped to about ten, and the engine knows more. The card
 * must still render something rather than an empty box or the raw key.
 */
export const UndescribedYoga: Story = {
  args: {
    yoga: {
      name: 'not_in_the_corpus',
      strength: 'moderate',
      involvedPlanets: ['Venus'],
      involvedHouses: [2],
    },
  },
}
