'use client'

import { Suspense, useCallback, useEffect, useRef, useState } from 'react'
import Link from 'next/link'
import { useRouter, useSearchParams } from 'next/navigation'

import { Wordmark } from '@/components/Logo'
import { OTPInput } from '@/components/OTPInput'
import { Button } from '@/components/ui/button'
import { authApi, AuthError, type Channel } from '@/lib/auth-api'
import { getDictionary, interpolate } from '@/lib/i18n/dictionaries'

const CODE_LENGTH = 6

/**
 * Resend backoff, per spec §5: 30s → 60s → 300s.
 *
 * Each send costs money on the SMS channel and patience on every
 * channel. After the third, the UI stops offering the same button and
 * suggests the other channel instead — repeating something that is
 * evidently not working for this person is not help.
 */
const RESEND_DELAYS = [30, 60, 300]

export default function VerifyPage() {
  // useSearchParams needs a Suspense boundary in the App Router, or the
  // whole route opts out of static rendering with a build-time error.
  return (
    <Suspense fallback={<VerifyFallback />}>
      <VerifyForm />
    </Suspense>
  )
}

function VerifyFallback() {
  return (
    <main id="main" className="flex min-h-dvh flex-col items-center justify-center px-6">
      <Wordmark />
    </main>
  )
}

function VerifyForm() {
  const router = useRouter()
  const params = useSearchParams()
  const t = getDictionary('en')

  const channel = (params.get('channel') ?? 'email') as Channel
  const identifier = params.get('identifier') ?? ''

  const [code, setCode] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [verifying, setVerifying] = useState(false)
  const [sendCount, setSendCount] = useState(1)
  const [secondsLeft, setSecondsLeft] = useState(RESEND_DELAYS[0] ?? 30)

  // Guards against the double submit that auto-submit-on-complete makes
  // easy: paste fires onComplete, and a fast Enter can fire it again
  // before the first request settles.
  const inFlight = useRef(false)

  // Arriving here without an identifier means a hand-typed URL or a lost
  // navigation. Send them back rather than showing a form that cannot work.
  useEffect(() => {
    if (!identifier) router.replace('/auth')
  }, [identifier, router])

  useEffect(() => {
    if (secondsLeft <= 0) return
    const timer = setInterval(() => setSecondsLeft((s) => Math.max(0, s - 1)), 1000)
    return () => clearInterval(timer)
  }, [secondsLeft])

  const submit = useCallback(
    async (value: string) => {
      if (inFlight.current || value.length !== CODE_LENGTH) return
      inFlight.current = true
      setVerifying(true)
      setError(null)

      try {
        const result = await authApi.verifyOTP(channel, identifier, value)
        // A new account goes to onboarding; a returning one goes home.
        // Driven by is_new_user rather than by whether a name is set, so
        // someone who skipped onboarding once is not sent round again.
        router.replace(result.is_new_user ? '/onboarding/name' : '/home')
      } catch (err) {
        setCode('')
        if (err instanceof AuthError) {
          if (err.code === 'RATE_LIMITED') {
            setError(t.verify.tooManyAttempts)
          } else if (err.status === 401) {
            setError(t.verify.incorrect)
          } else {
            setError(t.common.somethingWentWrong)
          }
        } else {
          setError(t.common.somethingWentWrong)
        }
        setVerifying(false)
        inFlight.current = false
      }
    },
    [channel, identifier, router, t],
  )

  async function resend() {
    setError(null)
    setCode('')
    try {
      await authApi.requestOTP(channel, identifier, 'en')
      const next = sendCount
      setSendCount(next + 1)
      setSecondsLeft(RESEND_DELAYS[Math.min(next, RESEND_DELAYS.length - 1)] ?? 300)
    } catch (err) {
      setError(
        err instanceof AuthError && err.code === 'RATE_LIMITED'
          ? t.verify.rateLimited
          : t.common.somethingWentWrong,
      )
    }
  }

  const exhaustedResends = sendCount > RESEND_DELAYS.length

  return (
    <main id="main" className="flex min-h-dvh flex-col items-center justify-center px-6 py-16">
      <Link href="/" aria-label="Ayana home">
        <Wordmark />
      </Link>

      <div className="mt-10 w-full max-w-sm">
        <h1 className="text-center font-serif text-3xl tracking-tight">{t.verify.title}</h1>
        <p className="mt-3 text-balance text-center text-sm text-ink-muted">
          {t.verify.sentTo}{' '}
          <span className="text-ink">{identifier}</span>
        </p>

        <form
          className="mt-8"
          onSubmit={(e) => {
            e.preventDefault()
            void submit(code)
          }}
        >
          <OTPInput
            length={CODE_LENGTH}
            value={code}
            onChange={(v) => {
              setCode(v)
              if (error) setError(null)
            }}
            onComplete={(v) => void submit(v)}
            disabled={verifying}
            invalid={Boolean(error)}
            label={t.verify.title}
            // The one place autofocus earns its keep, and the exception
            // is scoped to this element rather than the rule being
            // switched off.
            //
            // The user arrived here having just asked for a code; typing
            // it is the page's entire purpose and there is nothing above
            // to read first. Without focus, mobile users take an extra
            // tap at the highest drop-off point in the funnel — and the
            // OS one-time-code suggestion only appears on a focused
            // field, so the affordance that makes this painless does not
            // fire at all.
            // eslint-disable-next-line jsx-a11y/no-autofocus
            autoFocus
          />

          {error && (
            <p role="alert" className="mt-4 text-center text-sm text-danger">
              {error}
            </p>
          )}

          <Button
            type="submit"
            size="lg"
            disabled={verifying || code.length !== CODE_LENGTH}
            className="mt-6 w-full"
          >
            {verifying ? t.verify.verifying : t.common.continue}
          </Button>
        </form>

        <div className="mt-6 text-center">
          {exhaustedResends ? (
            // Three sends have not worked. Offering the same button a
            // fourth time is not help; offering the other channel is.
            <Link
              href={`/auth?channel=${channel === 'email' ? 'phone' : 'email'}`}
              className="text-sm text-gold underline underline-offset-4 hover:text-gold-soft"
            >
              {channel === 'phone' ? t.verify.tryEmailInstead : t.auth.usePhone}
            </Link>
          ) : secondsLeft > 0 ? (
            // A countdown, not a bare disabled button: the user needs to
            // know it will come back, and when.
            <p className="text-sm text-ink-faint" aria-live="polite">
              {interpolate(t.verify.resendIn, { seconds: secondsLeft })}
            </p>
          ) : (
            <button
              type="button"
              onClick={() => void resend()}
              className="rounded-lg px-3 py-1 text-sm text-gold underline underline-offset-4 hover:text-gold-soft"
            >
              {t.verify.resend}
            </button>
          )}
        </div>

        <div className="mt-8 text-center">
          <Link
            href="/auth"
            className="text-sm text-ink-muted underline underline-offset-4 hover:text-ink"
          >
            {t.verify.changeIdentifier}
          </Link>
        </div>
      </div>
    </main>
  )
}
