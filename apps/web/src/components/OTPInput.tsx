'use client'

import { useCallback, useEffect, useRef, useState } from 'react'
import { cn } from '@/lib/utils'

/**
 * Six-box OTP entry.
 *
 * The spec singles this out, and it is right to: this sits at the very
 * top of the funnel, and every interaction it gets wrong costs real
 * conversion. The details below are each there because their absence is
 * a known failure:
 *
 *   • autocomplete="one-time-code" — iOS and Android offer the code from
 *     the SMS on the keyboard. Without it the user switches apps to read
 *     it, and some of them do not come back.
 *   • Paste anywhere — people copy the whole code. Pasting "482913" into
 *     box 3 must fill all six, not put "482913" in one box.
 *   • Backspace on an empty box moves left. Otherwise correcting a typo
 *     means reaching for the mouse.
 *   • inputMode="numeric" — a numeric keypad on mobile rather than a
 *     full keyboard.
 *
 * One visually-hidden input backs the whole thing rather than six real
 * ones. Six inputs fight the platform: password managers try to fill
 * them individually, autofill targets only the first, and screen readers
 * announce "edit text, blank" six times. A single input with six painted
 * boxes gets native autofill and one coherent announcement.
 */

interface OTPInputProps {
  length?: number
  value: string
  onChange: (value: string) => void
  /** Fired when the last digit lands, so the form can submit itself. */
  onComplete?: (value: string) => void
  disabled?: boolean
  /** Describes the field for screen readers. */
  label: string
  invalid?: boolean
  autoFocus?: boolean
}

export function OTPInput({
  length = 6,
  value,
  onChange,
  onComplete,
  disabled = false,
  label,
  invalid = false,
  autoFocus = false,
}: OTPInputProps) {
  const inputRef = useRef<HTMLInputElement>(null)
  const [focused, setFocused] = useState(false)

  useEffect(() => {
    if (autoFocus) inputRef.current?.focus()
  }, [autoFocus])

  const handleChange = useCallback(
    (raw: string) => {
      // Strip everything that is not a digit. This is what makes paste
      // work for the formats people actually copy — "482 913",
      // "482-913", or a whole SMS line.
      const digits = raw.replace(/\D/g, '').slice(0, length)
      onChange(digits)
      if (digits.length === length) onComplete?.(digits)
    },
    [length, onChange, onComplete],
  )

  const digits = value.split('')
  // The caret sits on the first empty box, or the last one when full.
  const activeIndex = Math.min(value.length, length - 1)

  return (
    <div
      className="relative"
      onClick={() => inputRef.current?.focus()}
      // A click anywhere in the row focuses the real input. Without this
      // the boxes look interactive and are not.
      role="presentation"
    >
      <input
        ref={inputRef}
        type="text"
        inputMode="numeric"
        // The attribute that makes the OS offer the SMS code.
        autoComplete="one-time-code"
        // NO maxLength. It counts raw characters, so a pasted "482-913"
        // is truncated to "482-91" BEFORE the handler runs, and stripping
        // separators then leaves five digits and a broken code. The cap
        // that matters is the slice in handleChange, which counts digits
        // after filtering — which is the thing actually being limited.
        value={value}
        disabled={disabled}
        aria-label={label}
        aria-invalid={invalid || undefined}
        onChange={(e) => handleChange(e.target.value)}
        onFocus={() => setFocused(true)}
        onBlur={() => setFocused(false)}
        // Visually hidden but NOT display:none — a hidden input receives
        // no autofill and cannot be focused.
        className="absolute inset-0 h-full w-full cursor-pointer opacity-0"
      />

      <div className="flex justify-center gap-2 sm:gap-3" aria-hidden="true">
        {Array.from({ length }).map((_, i) => {
          const isActive = focused && i === activeIndex && !disabled
          const filled = digits[i] !== undefined

          return (
            <div
              key={i}
              className={cn(
                'flex h-14 w-11 items-center justify-center rounded-lg border text-2xl font-medium tabular-nums transition-colors sm:h-16 sm:w-13',
                'bg-surface text-ink',
                invalid
                  ? 'border-danger'
                  : isActive
                    ? 'border-gold ring-2 ring-ring ring-offset-2 ring-offset-background'
                    : filled
                      ? 'border-border'
                      : 'border-input',
                disabled && 'opacity-50',
              )}
            >
              {digits[i] ?? ''}
              {/* A caret in the active box, so the row reads as one
                  field being typed into rather than six separate ones. */}
              {isActive && !filled && (
                <span className="h-7 w-px animate-pulse bg-gold motion-reduce:animate-none" />
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}
