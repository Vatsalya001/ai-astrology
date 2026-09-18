import Link from 'next/link'
import type { Metadata } from 'next'
import { Wordmark } from '@/components/Logo'

export const metadata: Metadata = { title: 'Privacy — Ayana' }

/**
 * Describes what the system actually does, and can be checked against
 * the code. Every claim here corresponds to something enforced:
 * migration 000002 for what is stored, internal/platform/audit for what
 * is logged, and users.Deleter for deletion.
 *
 * A privacy policy that does not match the implementation is worse than
 * none, because people rely on it.
 */
export default function PrivacyPage() {
  return (
    <main id="main" tabIndex={-1} className="mx-auto max-w-2xl px-6 py-16">
      <Link href="/" aria-label="Ayana home">
        <Wordmark />
      </Link>

      <h1 className="mt-10 font-serif text-4xl tracking-tight">Privacy</h1>
      <p className="mt-2 text-sm text-ink-faint">Pre-launch. Last updated 14 September 2026.</p>

      <div className="mt-8 space-y-6 text-sm leading-relaxed text-ink-muted">
        <section>
          <h2 className="mb-2 font-serif text-xl text-ink">What we store</h2>
          <p>
            Your email address or phone number, whichever you signed up with,
            and a name if you give one. Your preferences. A list of the devices
            you are signed in on. From Phase 2, your birth date, time and place
            — which we treat as sensitive, because in combination they identify
            a person.
          </p>
        </section>

        <section>
          <h2 className="mb-2 font-serif text-xl text-ink">What we don&apos;t</h2>
          <p>
            We never store a password, because there isn&apos;t one. Sign-in
            codes are stored hashed and deleted the moment they are used. IP
            addresses are hashed before storage, never kept raw. Our audit log
            records what happened and to which account ID — never the contents
            of anything.
          </p>
        </section>

        <section>
          <h2 className="mb-2 font-serif text-xl text-ink">Deletion means deletion</h2>
          <p>
            You can delete your account from settings. After a seven-day grace
            period, during which you can change your mind, the rows are removed
            from the database. Not flagged, not hidden — removed. A record that
            a deletion happened survives, containing an account ID and a date
            and nothing else.
          </p>
        </section>

        <section>
          <h2 className="mb-2 font-serif text-xl text-ink">Taking your data with you</h2>
          <p>
            Settings offers a complete export of everything held about you, as a
            JSON file. Complete means complete — if a table holds your data, it
            is in the export.
          </p>
        </section>

        <section>
          <h2 className="mb-2 font-serif text-xl text-ink">AI</h2>
          <p>
            Development and testing use invented birth data only. Real user data
            never reaches a free model tier — the service refuses to start in a
            configuration that would allow it.
          </p>
        </section>
      </div>
    </main>
  )
}
