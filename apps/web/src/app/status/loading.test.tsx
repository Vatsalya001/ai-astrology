import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'

import StatusLoading from './loading'

/**
 * The skeleton's job is to occupy the same space the real content will,
 * so nothing jumps when the data arrives. See ADR-009.
 *
 * jsdom cannot measure layout, so these assert structure and count —
 * the things that make the shape right — and leave geometry to the eye.
 */

describe('status loading skeleton', () => {
  it('announces itself to assistive technology', () => {
    render(<StatusLoading />)

    // A screen reader user gets silence otherwise: the skeleton is
    // decorative, so without this there is nothing to announce and the
    // page appears to have loaded empty.
    expect(screen.getByText(/loading system status/i)).toBeInTheDocument()

    const main = screen.getByRole('main')
    expect(main).toHaveAttribute('aria-busy', 'true')
  })

  it('hides the decorative placeholders from the accessibility tree', () => {
    const { container } = render(<StatusLoading />)

    // Roughly thirty grey rectangles announced one by one is worse than
    // no loading state at all.
    const placeholders = container.querySelectorAll('.animate-pulse')
    expect(placeholders.length).toBeGreaterThan(0)

    for (const el of placeholders) {
      expect(el.closest('[aria-hidden="true"]')).not.toBeNull()
    }
  })

  it('reserves a row for every dependency the API reports', () => {
    const { container } = render(<StatusLoading />)

    // Six: postgres, redis, astro, ai, storage, mail. A mismatch here is
    // what makes the page jump when real data replaces the skeleton.
    const rows = container.querySelectorAll('.card.p-4')
    expect(rows).toHaveLength(6)
  })

  it('keeps the headings that are already known, rather than blanking them', () => {
    render(<StatusLoading />)

    // The title does not depend on the fetch, so hiding it behind a grey
    // bar loses information the page already has.
    expect(
      screen.getByRole('heading', { name: 'System status' }),
    ).toBeInTheDocument()
    expect(screen.getByText('Dependencies')).toBeInTheDocument()
  })

  it('respects prefers-reduced-motion', () => {
    const { container } = render(<StatusLoading />)

    // A full viewport of pulsing blocks is exactly the repetitive motion
    // the media query exists to suppress. Tailwind's global rule collapses
    // the duration, but the component opts out explicitly too, so the
    // skeleton stays correct if that rule is ever scoped down.
    for (const el of container.querySelectorAll('.animate-pulse')) {
      expect(el.className).toContain('motion-reduce:animate-none')
    }
  })
})
