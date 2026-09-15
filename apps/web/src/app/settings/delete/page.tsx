'use client'

import { useState } from 'react'
import { useRouter } from 'next/navigation'

import { Panel } from '@/components/ui'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { resetAnalytics, track } from '@/lib/analytics'
import { useLocale } from '@/lib/i18n/context'
import { AuthError } from '@/lib/auth-api'
import { usersApi } from '@/lib/users-api'

/**
 * Account deletion, and data export alongside it.
 *
 * The export sits on this screen deliberately: the moment someone
 * decides to leave is exactly when they should be offered their data,
 * not after it has gone.
 *
 * Two steps and a typed confirmation, per spec §6. A fresh code is
 * required on top of a valid session — an access token lives fifteen
 * minutes and an unlocked laptop is enough to use one, which is not the
 * bar for erasing an account.
 */
export default function DeleteAccountPage() {
  const { t, fill } = useLocale()
  const router = useRouter()

  const [step, setStep] = useState<'start' | 'confirm' | 'scheduled'>('start')
  const [code, setCode] = useState('')
  const [confirmWord, setConfirmWord] = useState('')
  const [scheduledFor, setScheduledFor] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const confirmed = confirmWord === t.settings.deleteConfirmWord

  async function sendCode() {
    setBusy(true)
    setError(null)
    try {
      await usersApi.challenge()
      setStep('confirm')
    } catch {
      setError(t.common.somethingWentWrong)
    } finally {
      setBusy(false)
    }
  }

  async function confirmDelete(e: React.FormEvent) {
    e.preventDefault()
    if (!confirmed) return

    setBusy(true)
    setError(null)
    try {
      const me = await usersApi.me()
      const { deletion_scheduled_at } = await usersApi.requestDeletion(code)
      track('account_deletion_requested', { user_id: me.id })
      setScheduledFor(deletion_scheduled_at.slice(0, 10))
      setStep('scheduled')
      // Requesting deletion revokes every session server-side, so this
      // tab is already signed out. Clearing locally keeps the UI honest
      // rather than showing a logged-in shell that 401s on every call.
      resetAnalytics()
    } catch (err) {
      setError(
        err instanceof AuthError && err.status === 401
          ? t.verify.incorrect
          : t.common.somethingWentWrong,
      )
      setBusy(false)
    }
  }

  if (step === 'scheduled') {
    return (
      <div>
        <h2 className="font-serif text-2xl">{t.settings.deleteTitle}</h2>
        <Panel className="mt-6 border-warn/35 bg-warn/[0.06]">
          <p className="text-sm">{fill(t.settings.deleteScheduled, { date: scheduledFor })}</p>
          <p className="mt-2 text-sm text-ink-muted">{t.settings.deleteGrace}</p>
        </Panel>
        <Button className="mt-6" onClick={() => router.replace('/')}>
          {t.nav.home}
        </Button>
      </div>
    )
  }

  return (
    <div>
      {/* Export first, and above the destructive action rather than
          below it — after someone has typed DELETE they are not reading
          any more. */}
      <Panel>
        <h2 className="font-serif text-xl">{t.settings.exportTitle}</h2>
        <p className="mt-2 text-sm text-ink-muted">{t.settings.exportBody}</p>
        <Button
          variant="secondary"
          className="mt-4"
          disabled={busy}
          onClick={async () => {
            setBusy(true)
            setError(null)
            try {
              await usersApi.challenge()
              setStep('confirm')
            } catch {
              setError(t.common.somethingWentWrong)
            } finally {
              setBusy(false)
            }
          }}
        >
          {t.settings.deleteExportFirst}
        </Button>
      </Panel>

      <h2 className="mt-10 font-serif text-2xl text-danger">{t.settings.deleteTitle}</h2>
      <p className="mt-2 text-sm text-ink-muted">{t.settings.deleteBody}</p>
      <p className="mt-2 text-sm text-ink-muted">{t.settings.deleteGrace}</p>

      {step === 'start' ? (
        <Button variant="destructive" className="mt-6" disabled={busy} onClick={() => void sendCode()}>
          {t.settings.deleteSendCode}
        </Button>
      ) : (
        <form onSubmit={confirmDelete} className="mt-6" noValidate>
          <label htmlFor="code" className="mb-2 block text-sm font-medium">
            {t.settings.deleteCodeLabel}
          </label>
          <Input
            id="code"
            inputMode="numeric"
            autoComplete="one-time-code"
            value={code}
            disabled={busy}
            onChange={(e) => setCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
            className="h-12 max-w-[12rem] text-base tracking-[0.3em]"
          />

          <label htmlFor="confirm" className="mb-2 mt-6 block text-sm font-medium">
            {t.settings.deleteConfirmLabel}
          </label>
          <Input
            id="confirm"
            value={confirmWord}
            disabled={busy}
            // Deliberately no autocapitalize help and no paste assist:
            // typing the word is the point. A one-tap confirmation is
            // not a confirmation.
            autoComplete="off"
            onChange={(e) => setConfirmWord(e.target.value)}
            className="h-12 max-w-[16rem] text-base"
          />

          {error && (
            <p role="alert" className="mt-4 text-sm text-danger">
              {error}
            </p>
          )}

          <div className="mt-8 flex flex-wrap gap-3">
            <Button
              type="submit"
              variant="destructive"
              disabled={busy || !confirmed || code.length !== 6}
            >
              {t.settings.deleteButton}
            </Button>
            <Button
              type="button"
              variant="secondary"
              disabled={busy || code.length !== 6}
              onClick={() => {
                // A plain navigation so the browser saves the file.
                window.location.href = usersApi.exportUrl(code)
                void usersApi.me().then((me) => track('data_exported', { user_id: me.id }))
              }}
            >
              {t.settings.exportButton}
            </Button>
          </div>
        </form>
      )}
    </div>
  )
}
