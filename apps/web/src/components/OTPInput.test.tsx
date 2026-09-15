import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

import { OTPInput } from './OTPInput'

/**
 * The spec singles this component out, and every assertion below maps to
 * a known way of losing people at the top of the funnel.
 *
 * jsdom cannot tell us whether iOS actually offers the SMS code — that
 * is the OS reading `autocomplete="one-time-code"` off a focused field —
 * so what is testable is that the attribute is present and correct.
 * Losing it is silent and costs conversion nobody attributes to a diff.
 */

describe('the attributes that make mobile entry painless', () => {
  it('declares one-time-code so the OS offers the SMS code', () => {
    render(<OTPInput value="" onChange={() => {}} label="Code" />)
    const input = screen.getByLabelText('Code')

    // Without this the user switches apps to read the code, and some of
    // them do not come back.
    expect(input).toHaveAttribute('autocomplete', 'one-time-code')
  })

  it('requests a numeric keypad rather than a full keyboard', () => {
    render(<OTPInput value="" onChange={() => {}} label="Code" />)
    expect(screen.getByLabelText('Code')).toHaveAttribute('inputmode', 'numeric')
  })

  // maxLength is deliberately absent: it counts raw characters, so a
  // pasted "482-913" would be cut to "482-91" before the handler could
  // strip the separator — five digits and a broken code, for the most
  // common way people enter one.
  it('does not cap raw input length, which would break formatted pastes', () => {
    render(<OTPInput value="" onChange={() => {}} label="Code" length={6} />)
    expect(screen.getByLabelText('Code')).not.toHaveAttribute('maxlength')
  })
})

describe('input handling', () => {
  it('accepts typed digits', async () => {
    const onChange = vi.fn()
    render(<OTPInput value="" onChange={onChange} label="Code" />)

    await userEvent.type(screen.getByLabelText('Code'), '4')
    expect(onChange).toHaveBeenCalledWith('4')
  })

  // People copy the whole code. Pasting "482913" must fill all six boxes,
  // not put the string in one.
  it('accepts a pasted code', async () => {
    const onChange = vi.fn()
    render(<OTPInput value="" onChange={onChange} label="Code" />)

    const input = screen.getByLabelText('Code')
    await userEvent.click(input)
    await userEvent.paste('482913')

    expect(onChange).toHaveBeenCalledWith('482913')
  })

  // Codes get copied out of an SMS with whatever formatting came along.
  it('strips separators and spaces from a paste', async () => {
    const onChange = vi.fn()
    render(<OTPInput value="" onChange={onChange} label="Code" />)

    await userEvent.click(screen.getByLabelText('Code'))
    await userEvent.paste('482-913')

    expect(onChange).toHaveBeenLastCalledWith('482913')
  })

  it('ignores non-digits entirely', async () => {
    const onChange = vi.fn()
    render(<OTPInput value="" onChange={onChange} label="Code" />)

    await userEvent.click(screen.getByLabelText('Code'))
    await userEvent.paste('abc482913xyz')

    expect(onChange).toHaveBeenLastCalledWith('482913')
  })

  it('truncates a paste longer than the code', async () => {
    const onChange = vi.fn()
    render(<OTPInput value="" onChange={onChange} label="Code" length={6} />)

    await userEvent.click(screen.getByLabelText('Code'))
    await userEvent.paste('4829137777')

    expect(onChange).toHaveBeenLastCalledWith('482913')
  })

  // The form submits itself on the last digit, so nobody has to hunt for
  // a button after typing six numbers.
  it('fires onComplete when the last digit lands', async () => {
    const onComplete = vi.fn()
    render(<OTPInput value="" onChange={() => {}} onComplete={onComplete} label="Code" />)

    await userEvent.click(screen.getByLabelText('Code'))
    await userEvent.paste('482913')

    expect(onComplete).toHaveBeenCalledWith('482913')
  })

  it('does not fire onComplete on a partial code', async () => {
    const onComplete = vi.fn()
    render(<OTPInput value="" onChange={() => {}} onComplete={onComplete} label="Code" />)

    await userEvent.click(screen.getByLabelText('Code'))
    await userEvent.paste('4829')

    expect(onComplete).not.toHaveBeenCalled()
  })
})

describe('what it renders', () => {
  it('paints one box per digit', () => {
    const { container } = render(<OTPInput value="" onChange={() => {}} label="Code" length={6} />)
    // The boxes are decorative; the real input is the single hidden one.
    const boxes = container.querySelectorAll('[aria-hidden="true"] > div')
    expect(boxes).toHaveLength(6)
  })

  it('shows the digits entered so far', () => {
    render(<OTPInput value="482" onChange={() => {}} label="Code" />)
    for (const digit of ['4', '8', '2']) {
      expect(screen.getByText(digit)).toBeInTheDocument()
    }
  })

  // One input, not six. Six fight the platform: password managers fill
  // them individually, autofill targets only the first, and a screen
  // reader announces "edit text, blank" six times.
  it('exposes exactly one field to assistive technology', () => {
    const { container } = render(<OTPInput value="" onChange={() => {}} label="Code" />)
    expect(container.querySelectorAll('input')).toHaveLength(1)
  })

  it('marks itself invalid so the error is associated', () => {
    render(<OTPInput value="482" onChange={() => {}} label="Code" invalid />)
    expect(screen.getByLabelText('Code')).toHaveAttribute('aria-invalid', 'true')
  })

  it('disables the real input when disabled', () => {
    render(<OTPInput value="" onChange={() => {}} label="Code" disabled />)
    expect(screen.getByLabelText('Code')).toBeDisabled()
  })
})
