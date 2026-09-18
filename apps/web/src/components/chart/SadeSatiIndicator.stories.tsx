import type { Meta, StoryObj } from '@storybook/nextjs-vite'

import { SadeSatiIndicator } from './SadeSatiIndicator'

/**
 * Sade Sati.
 *
 * Saturn returns every ~29.5 years and the stretch lasts about seven and
 * a half, so at any moment roughly three of twelve Moon signs are in it.
 * That means most of these states are unreachable in the running app on
 * any given day — you cannot see the "rising" phase in March if Saturn
 * is setting.
 *
 * Which is exactly what Storybook is for here.
 */
const meta = {
  title: 'Chart/SadeSatiIndicator',
  component: SadeSatiIndicator,
} satisfies Meta<typeof SadeSatiIndicator>

export default meta
type Story = StoryObj<typeof meta>

/** The server's instant, fixed so "years left" does not drift. */
const AT = '2026-09-18T00:00:00Z'

export const NotRunning: Story = {
  args: {
    at: AT,
    status: {
      is_active: false,
      phase: null,
      saturn_sign: 'Leo',
      houses_from_moon: 7,
      started_at: null,
      ends_at: null,
    },
  },
}

export const Rising: Story = {
  args: {
    at: AT,
    status: {
      is_active: true,
      phase: 'rising',
      saturn_sign: 'Aquarius',
      houses_from_moon: 12,
      started_at: '2026-04-01T00:00:00Z',
      ends_at: '2033-09-12T00:00:00Z',
    },
  },
}

export const Peak: Story = {
  args: {
    at: AT,
    status: {
      is_active: true,
      phase: 'peak',
      saturn_sign: 'Pisces',
      houses_from_moon: 1,
      started_at: '2022-04-29T00:00:00Z',
      ends_at: '2030-04-17T00:00:00Z',
    },
  },
}

export const Setting: Story = {
  args: {
    at: AT,
    status: {
      is_active: true,
      phase: 'setting',
      saturn_sign: 'Aries',
      houses_from_moon: 2,
      started_at: '2020-01-24T00:00:00Z',
      ends_at: '2028-02-23T00:00:00Z',
    },
  },
}

/**
 * Running, but the dates are not known.
 *
 * A fresh database before the worker's first pass sends nulls with
 * `is_active: true`. The panel must say it does not know — rendering
 * nothing there reads as "this has no end", which is both untrue and
 * the more frightening of the two readings.
 */
export const DatesUnknown: Story = {
  args: {
    at: AT,
    status: {
      is_active: true,
      phase: 'peak',
      saturn_sign: 'Pisces',
      houses_from_moon: 1,
      started_at: null,
      ends_at: null,
    },
  },
}

/**
 * Nearly over.
 *
 * The copy switches from "about N years left" to "less than a year",
 * which is a branch with its own string and therefore its own way to be
 * wrong.
 */
export const EndingSoon: Story = {
  args: {
    at: AT,
    status: {
      is_active: true,
      phase: 'setting',
      saturn_sign: 'Aries',
      houses_from_moon: 2,
      started_at: '2019-06-01T00:00:00Z',
      ends_at: '2027-01-15T00:00:00Z',
    },
  },
}
