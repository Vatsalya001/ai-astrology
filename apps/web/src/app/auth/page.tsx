'use client'

import { useState } from 'react'
import Link from 'next/link'
import { useRouter } from 'next/navigation'

import { Wordmark } from '@/components/Logo'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { authApi, AuthError, type Channel } from '@/lib/auth-api'
import { getDictionary } from '@/lib/i18n/dictionaries'
import { cn } from '@/lib/utils'

/**
 * Sign in and sign up, as one flow.
 *
 * Never make someone guess whether they already have an account. One
 * field, one button; the branch happens server-side and the user finds
 * out from where they land afterwards, not from which form they picked.
 *
 * A client component because it holds form state. The page it replaces
 * on the server renders nothing interactive.
 */
export default function AuthPage() {
  const router = useRouter()
  const t = getDictionary('en')

  const [channel, setChannel] = useState<Channel>('email')
  const [identifier, setIdentifier] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  const isEmail = channel === 'email'

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setError(null)

    const trimmed = identifier.trim()
    if (!looksValid(channel, trimmed)) {
      setError(isEmail ? t.auth.invalidEmail : t.auth.invalidPhone)
      return
    }

    setSubmitting(true)
    try {
      await authApi.requestOTP(channel, trimmed, 'en')

      // The identifier travels in the URL rather than in storage: the
      // verify page needs it, it is not a secret, and a page refresh
      // must not drop the user back to the start.
      router.push(
        `/auth/verify?channel=${channel}&identifier=${encodeURIComponent(trimmed)}`,
      )
    } catch (err) {
      if (err instanceof AuthError && err.code === 'RATE_LIMITED') {
        setError(
          err.retryAfter
            ? `Too many requests. Try again in ${formatWait(err.retryAfter)}.`
            : t.verify.rateLimited,
        )
      } else if (err instanceof AuthError && err.code === 'VALIDATION_FAILED') {
        setError(isEmail ? t.auth.invalidEmail : t.auth.invalidPhone)
      } else {
        setError(t.common.somethingWentWrong)
      }
      setSubmitting(false)
    }
  }

  return (
    <main id="main" className="flex min-h-dvh flex-col items-center justify-center px-6 py-16">
      <Link href="/" aria-label="Ayana home">
        <Wordmark />
      </Link>

      <div className="mt-10 w-full max-w-sm">
        <h1 className="text-balance text-center font-serif text-3xl tracking-tight">
          {t.auth.title}
        </h1>
        <p className="mt-3 text-balance text-center text-sm text-ink-muted">
          {t.auth.subtitle}
        </p>

        <form onSubmit={handleSubmit} className="mt-8" noValidate>
          <label htmlFor="identifier" className="mb-2 block text-sm font-medium">
            {isEmail ? t.auth.emailLabel : t.auth.phoneLabel}
          </label>

          <Input
            id="identifier"
            name="identifier"
            type={isEmail ? 'email' : 'tel'}
            inputMode={isEmail ? 'email' : 'tel'}
            // Lets the browser and password managers offer the value the
            // user has used here before.
            autoComplete={isEmail ? 'email' : 'tel'}
            // No autoFocus. There is a heading and a subtitle above this
            // field that a screen reader would talk over, and on mobile
            // the keyboard would cover half the page — including the
            // "use phone instead" switch — before anything has been read.
            required
            disabled={submitting}
            placeholder={isEmail ? t.auth.emailPlaceholder : t.auth.phonePlaceholder}
            value={identifier}
            onChange={(e) => {
              setIdentifier(e.target.value)
              if (error) setError(null)
            }}
            aria-invalid={error ? true : undefined}
            aria-describedby={error ? 'identifier-error' : undefined}
            className={cn('h-12 text-base', error && 'border-danger')}
          />

          {/* role="alert" so the message is announced when it appears,
              rather than being silently added below a field the user has
              already moved past. */}
          {error && (
            <p id="identifier-error" role="alert" className="mt-2 text-sm text-danger">
              {error}
            </p>
          )}

          <Button type="submit" size="lg" disabled={submitting} className="mt-5 w-full">
            {submitting ? t.auth.sending : t.common.continue}
          </Button>
        </form>

        <button
          type="button"
          onClick={() => {
            setChannel(isEmail ? 'phone' : 'email')
            setIdentifier('')
            setError(null)
          }}
          disabled={submitting}
          className="mt-5 w-full rounded-lg py-2 text-sm text-ink-muted transition-colors hover:text-ink disabled:opacity-50"
        >
          {isEmail ? t.auth.usePhone : t.auth.useEmail}
        </button>

        <p className="mt-8 text-balance text-center text-xs leading-relaxed text-ink-faint">
          {t.auth.termsPrefix}{' '}
          <Link href="/terms" className="underline underline-offset-2 hover:text-ink-muted">
            {t.auth.terms}
          </Link>{' '}
          {t.auth.and}{' '}
          <Link href="/privacy" className="underline underline-offset-2 hover:text-ink-muted">
            {t.auth.privacy}
          </Link>
          .
        </p>
      </div>
    </main>
  )
}

/**
 * Client-side shape check only.
 *
 * Deliberately looser than the server's: this exists to catch a typo
 * before a round trip, not to be the validation. The server re-validates
 * every field regardless, because anything enforced only in a browser is
 * not enforced.
 */
function looksValid(channel: Channel, value: string): boolean {
  if (channel === 'email') {
    return /^[^@\s]+@[^@\s.]+\.[^@\s]+$/.test(value)
  }
  return /^\+[1-9]\d{7,14}$/.test(value.replace(/[\s()-]/g, ''))
}

function formatWait(seconds: number): string {
  if (seconds < 60) return `${seconds} seconds`
  const minutes = Math.ceil(seconds / 60)
  return minutes === 1 ? 'a minute' : `${minutes} minutes`
}
