'use client'

import { Button } from '@/components/ui/button'
import { useLocale } from '@/lib/i18n/context'

/**
 * Shown when a screen's data could not be loaded for a reason that is
 * NOT "you are signed out".
 *
 * Until this existed, every screen sent every load failure to /auth. A
 * dropped connection or an API restart therefore looked identical to a
 * dead session: the user was thrown to the sign-in page, told nothing,
 * and — if the outage was still going — could not sign in either. The
 * reasonable conclusion from that is that their account is gone.
 *
 * `role="alert"` so a screen reader is told, rather than silently landing
 * on a page whose content changed underneath it.
 *
 * The retry button matters more than the message. Most of what lands here
 * is transient, and the fix is one tap — offering it is the difference
 * between a blip and a lost session.
 */
export function LoadError({ onRetry }: { onRetry: () => void }) {
  const { t } = useLocale()

  return (
    <div role="alert" className="py-8 text-center">
      <p className="text-sm text-ink-muted">{t.common.somethingWentWrong}</p>
      <Button variant="secondary" onClick={onRetry} className="mt-4">
        {t.common.tryAgain}
      </Button>
    </div>
  )
}
