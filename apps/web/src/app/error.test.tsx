import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

import ErrorBoundary from './error'

/**
 * See ADR-009 for why this is a component test rather than a browser one.
 *
 * The first block is the reason this file exists. The rest is the
 * difference between an error page and a dead end.
 */

beforeEach(() => {
  // The component logs deliberately; without this the suite output is
  // buried in stack traces that are expected.
  vi.spyOn(console, 'error').mockImplementation(() => {})
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('never exposes the underlying error', () => {
  // Every one of these is something a real server-render failure has
  // carried into an error object at some point.
  const secrets = [
    'postgres://ayana:hunter2@localhost:5433/ayana',
    'connect ECONNREFUSED 127.0.0.1:8100',
    'Bearer eyJhbGciOiJIUzI1NiIs.rest.of.it',
    'INTERNAL_TOKEN=s3cr3t',
  ]

  it.each(secrets)('does not render %s', (secret) => {
    const error = Object.assign(new Error(secret), { digest: 'abc123' })
    const { container } = render(<ErrorBoundary error={error} reset={() => {}} />)

    // Against the full DOM text, not a query: a "helpful" change that
    // surfaces the message in a tooltip, an aria-label or a details
    // element would slip past anything narrower.
    expect(container.textContent).not.toContain(secret)
    expect(container.innerHTML).not.toContain(secret)
  })

  it('does not render the stack trace', () => {
    const error = new Error('boom')
    error.stack = 'Error: boom\n    at /app/.next/server/chunks/ssr/secret.js:1:119'

    const { container } = render(<ErrorBoundary error={error} reset={() => {}} />)

    expect(container.textContent).not.toContain('.next/server')
    expect(container.textContent).not.toContain('at /app')
  })
})

describe('gives the user somewhere to go', () => {
  it('offers a retry that actually calls reset', async () => {
    const reset = vi.fn()
    render(<ErrorBoundary error={new Error('boom')} reset={reset} />)

    await userEvent.click(screen.getByRole('button', { name: /try again/i }))

    expect(reset).toHaveBeenCalledOnce()
  })

  it('offers a route home, so the page is never a dead end', () => {
    render(<ErrorBoundary error={new Error('boom')} reset={() => {}} />)

    expect(screen.getByRole('link', { name: /back to home/i })).toHaveAttribute(
      'href',
      '/',
    )
  })

  it('explains itself with a heading a screen reader can find', () => {
    render(<ErrorBoundary error={new Error('boom')} reset={() => {}} />)

    expect(
      screen.getByRole('heading', { name: /couldn't render this page/i }),
    ).toBeInTheDocument()
  })
})

describe('correlation with the server log', () => {
  it('shows the digest, which is what Next logs the stack under', () => {
    const error = Object.assign(new Error('boom'), { digest: '4042727749' })
    render(<ErrorBoundary error={error} reset={() => {}} />)

    // Without this the user has nothing to quote and the operator has no
    // way to find the trace — the page becomes an apology with no thread
    // back to the cause.
    expect(screen.getByText(/4042727749/)).toBeInTheDocument()
  })

  it('omits the reference entirely when there is no digest', () => {
    render(<ErrorBoundary error={new Error('boom')} reset={() => {}} />)

    // Rather than rendering "Reference: undefined".
    expect(screen.queryByText(/reference/i)).not.toBeInTheDocument()
  })
})
