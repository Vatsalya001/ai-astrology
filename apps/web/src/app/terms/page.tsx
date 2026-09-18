import Link from 'next/link'
import type { Metadata } from 'next'
import { Wordmark } from '@/components/Logo'

export const metadata: Metadata = { title: 'Terms — Ayana' }

/**
 * Deliberately short and deliberately honest.
 *
 * The sign-up screen asks people to agree to these, and a 404 behind
 * that link is asking someone to consent to something they cannot read.
 * Boilerplate copied from a template would be worse: it would describe a
 * service that does not exist yet and commitments nobody has reviewed.
 *
 * This says what is actually true today. It gets replaced by a lawyer's
 * version before launch, not by more text from me.
 */
export default function TermsPage() {
  return (
    <main id="main" tabIndex={-1} className="mx-auto max-w-2xl px-6 py-16">
      <Link href="/" aria-label="Ayana home">
        <Wordmark />
      </Link>

      <h1 className="mt-10 font-serif text-4xl tracking-tight">Terms of use</h1>
      <p className="mt-2 text-sm text-ink-faint">Pre-launch. Last updated 14 September 2026.</p>

      <div className="mt-8 space-y-6 text-sm leading-relaxed text-ink-muted">
        <p>
          Ayana is under construction and not yet a live service. These terms
          will be replaced by a reviewed version before it is offered to the
          public. Until then, the following is what actually applies.
        </p>

        <section>
          <h2 className="mb-2 font-serif text-xl text-ink">Guidance, not advice</h2>
          <p>
            Astrological readings are for reflection and entertainment. They are
            not a substitute for professional medical, legal, financial or
            psychological advice, and nothing here should be treated as a
            prediction of any outcome.
          </p>
        </section>

        <section>
          <h2 className="mb-2 font-serif text-xl text-ink">Your account</h2>
          <p>
            You are responsible for the contact details you sign up with. We do
            not use passwords — access is by a code sent to your email or phone,
            so keeping that inbox or number secure is what keeps your account
            secure.
          </p>
        </section>

        <section>
          <h2 className="mb-2 font-serif text-xl text-ink">Availability</h2>
          <p>
            This is a development build. Data may be reset without notice, and
            no availability is promised.
          </p>
        </section>

        <p>
          Questions:{' '}
          <Link href="/privacy" className="text-gold underline underline-offset-4">
            see also the Privacy Policy
          </Link>
          .
        </p>
      </div>
    </main>
  )
}
