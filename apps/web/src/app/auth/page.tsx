'use client'

import { Suspense, useEffect, useState } from 'react'
import Link from 'next/link'
import { useRouter, useSearchParams } from 'next/navigation'

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
  // useSearchParams needs a Suspense boundary in the App Router, or the
  // whole route opts out of static rendering with a build-time error.
  return (
    <Suspense fallback={<AuthShell />}>
      <AuthForm />
    </Suspense>
  )
}

function AuthForm() {
  const router = useRouter()
  const params = useSearchParams()
  const t = getDictionary('en')

  const [channel, setChannel] = useState<Channel>('email')
  const [identifier, setIdentifier] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  // Rendered only once the API confirms it. Starting at false and
  // revealing the button is right: a button that flashes and vanishes is
  // worse than one that arrives a moment late, and the alternative —
  // showing it optimistically — sends the first clicker to a 503.
  const [googleEnabled, setGoogleEnabled] = useState(false)

  const isEmail = channel === 'email'

  // The callback reports user-facing failures as a query parameter,
  // because it is a browser navigation returning from Google and cannot
  // deliver a JSON body to a page.
  const callbackError = params.get('error')

  useEffect(() => {
    let live = true
    void authApi.providers().then((p) => {
      if (live) setGoogleEnabled(p.google)
    })
    return () => {
      live = false
    }
  }, [])

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

        {callbackError && (
          <p
            role="alert"
            className="mt-6 rounded-lg border border-danger/40 bg-danger/10 px-4 py-3 text-sm text-danger"
          >
            {callbackError === 'cancelled' ? t.auth.cancelled : t.auth.unavailable}
          </p>
        )}

        {googleEnabled && (
          <>
            {/* A link, not a fetch: the browser must follow a redirect to
                Google's own origin, which CORS would never allow from
                JavaScript. `rel` is irrelevant here (same tab, trusted
                destination) but the anchor must not be prefetched — Next
                would consume a single-use state before the user clicks. */}
            <a
              href={authApi.oauthURL()}
              className="mt-8 flex h-12 w-full items-center justify-center gap-3 rounded-lg border border-input bg-surface text-sm font-medium transition-colors hover:bg-elevated focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
            >
              <GoogleMark />
              {t.auth.google}
            </a>

            <div className="mt-6 flex items-center gap-4" aria-hidden="true">
              <span className="h-px flex-1 bg-border" />
              <span className="text-xs uppercase tracking-widest text-ink-faint">
                {t.auth.or}
              </span>
              <span className="h-px flex-1 bg-border" />
            </div>
          </>
        )}

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
 * The page without its interactive form.
 *
 * Shown while Suspense resolves, so the heading and wordmark do not pop
 * in — the layout is identical, only the controls are missing.
 */
function AuthShell() {
  const t = getDictionary('en')
  return (
    <main
      id="main"
      className="flex min-h-dvh flex-col items-center justify-center px-6 py-16"
    >
      <Wordmark />
      <div className="mt-10 w-full max-w-sm">
        <h1 className="text-balance text-center font-serif text-3xl tracking-tight">
          {t.auth.title}
        </h1>
        <p className="mt-3 text-balance text-center text-sm text-ink-muted">
          {t.auth.subtitle}
        </p>
        <div className="mt-8 h-12 w-full animate-pulse rounded-lg bg-surface" />
        <div className="mt-5 h-12 w-full animate-pulse rounded-lg bg-surface" />
      </div>
    </main>
  )
}

/**
 * Google's mark, inline.
 *
 * Inline rather than an <img> from Google's CDN: a third-party request
 * from the sign-in page tells Google who is looking at it before anyone
 * has chosen to sign in with them.
 */
function GoogleMark() {
  return (
    <svg width="18" height="18" viewBox="0 0 18 18" aria-hidden="true" focusable="false">
      <path
        fill="#4285F4"
        d="M17.64 9.2c0-.64-.06-1.25-.16-1.84H9v3.48h4.84a4.14 4.14 0 0 1-1.8 2.72v2.26h2.92c1.7-1.57 2.68-3.88 2.68-6.62Z"
      />
      <path
        fill="#34A853"
        d="M9 18c2.43 0 4.47-.8 5.96-2.18l-2.92-2.26c-.8.54-1.84.86-3.04.86-2.34 0-4.32-1.58-5.03-3.7H.96v2.33A9 9 0 0 0 9 18Z"
      />
      <path
        fill="#FBBC05"
        d="M3.97 10.72a5.4 5.4 0 0 1 0-3.44V4.95H.96a9 9 0 0 0 0 8.1l3.01-2.33Z"
      />
      <path
        fill="#EA4335"
        d="M9 3.58c1.32 0 2.5.45 3.44 1.35l2.58-2.58C13.46.89 11.43 0 9 0A9 9 0 0 0 .96 4.95l3.01 2.33C4.68 5.16 6.66 3.58 9 3.58Z"
      />
    </svg>
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
