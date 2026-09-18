'use client'

import { useCallback, useState } from 'react'

import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { track } from '@/lib/analytics'
import { astrologyApi } from '@/lib/astrology-api'
import { useLocale } from '@/lib/i18n/context'
import { cn } from '@/lib/utils'

import { SHARE_IMAGE_FILENAME, svgToPng } from './shareImage'

/**
 * The share sheet: image, link, PDF.
 *
 * ── Three things, because people mean three different things ──
 *
 * An IMAGE goes straight into a WhatsApp thread and is what most people
 * want. A LINK stays live and can be revoked, which is what you send an
 * astrologer. A PDF is the artifact people print and keep.
 *
 * ── What the link deliberately is not ──
 *
 * It carries one opaque token and nothing else. No birth date, no time,
 * no place, no ids — the server resolves it and decides what to return,
 * against a row the owner can kill. That is why the warning below says
 * the chart is visible AND that the birth details are not: both halves
 * are true, and a reader deciding whether to send this needs both.
 */

type Phase = 'idle' | 'working' | 'link-ready' | 'failed' | 'too-many'

export function ShareSheet({
  profileId,
  /** A ref to the on-page chart SVG, for the image export. */
  chartRef,
  /** The page background, painted under the exported PNG. */
  imageBackground = '#0B1026',
  className,
}: {
  profileId: string
  chartRef: React.RefObject<SVGSVGElement | null>
  imageBackground?: string
  className?: string
}) {
  const { t } = useLocale()

  const [open, setOpen] = useState(false)
  const [phase, setPhase] = useState<Phase>('idle')
  const [link, setLink] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  const reset = useCallback(() => {
    setPhase('idle')
    setLink(null)
    setCopied(false)
  }, [])

  // ─── image ─────────────────────────────────────────────────────────

  const shareImage = useCallback(async () => {
    const svg = chartRef.current
    if (!svg) {
      setPhase('failed')
      return
    }

    setPhase('working')
    try {
      const png = await svgToPng(svg, imageBackground)
      const file = new File([png], SHARE_IMAGE_FILENAME, { type: 'image/png' })

      /*
        The Web Share API when the browser has it, a download otherwise.

        `canShare({ files })` rather than a feature check on `share`
        alone: Safari and several Android browsers expose `navigator.share`
        while refusing files, and calling it anyway throws a
        NotAllowedError that reads to the user as the button being broken.
      */
      if (typeof navigator.canShare === 'function' && navigator.canShare({ files: [file] })) {
        await navigator.share({ files: [file], title: t.chart.shareTitle })
        track('kundli_shared', { method: 'image' })
        setPhase('idle')
        return
      }

      downloadBlob(png, SHARE_IMAGE_FILENAME)
      track('kundli_shared', { method: 'image_download' })
      setPhase('idle')
    } catch (err) {
      /*
        A cancelled share is not a failure.

        Dismissing the OS share sheet rejects with an AbortError, and
        showing "that did not work" to somebody who simply changed their
        mind is the component calling them wrong.
      */
      if (err instanceof DOMException && err.name === 'AbortError') {
        setPhase('idle')
        return
      }
      setPhase('failed')
    }
  }, [chartRef, imageBackground, t.chart.shareTitle])

  // ─── link ──────────────────────────────────────────────────────────

  const createLink = useCallback(async () => {
    setPhase('working')
    setCopied(false)
    try {
      const share = await astrologyApi.createShare(profileId)
      if (!share.token) {
        // The API returns the token exactly once and only here. Without
        // it there is no link, and there is no way to ask again.
        setPhase('failed')
        return
      }

      // Built from the page's own origin rather than from anything the
      // server sent, so a compromised response cannot point a user's
      // share at another host.
      setLink(`${window.location.origin}/shared/${share.token}`)
      setPhase('link-ready')
      track('kundli_shared', { method: 'link' })
    } catch (err) {
      setPhase(isConflict(err) ? 'too-many' : 'failed')
    }
  }, [profileId])

  const copyLink = useCallback(async () => {
    if (!link) return
    try {
      await navigator.clipboard.writeText(link)
      setCopied(true)
    } catch {
      // Clipboard access can be refused outright. The input below is
      // readOnly rather than disabled and the text is selectable, so
      // there is still a way to get the link out by hand.
      setCopied(false)
    }
  }, [link])

  // ─── render ────────────────────────────────────────────────────────

  return (
    <>
      <Button
        type="button"
        variant="outline"
        className={className}
        onClick={() => {
          reset()
          setOpen(true)
        }}
      >
        {t.chart.shareOpen}
      </Button>

      <Dialog
        open={open}
        onOpenChange={(next) => {
          setOpen(next)
          if (!next) reset()
        }}
      >
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>{t.chart.shareTitle}</DialogTitle>
            <DialogDescription>{t.chart.shareLinkWarning}</DialogDescription>
          </DialogHeader>

          {phase === 'link-ready' && link ? (
            <LinkPanel
              link={link}
              copied={copied}
              onCopy={() => void copyLink()}
              copyLabel={copied ? t.chart.shareCopied : t.chart.shareCopy}
              readyLabel={t.chart.shareLinkReady}
              expiryLabel={t.chart.shareLinkExpires}
            />
          ) : (
            <div className="flex flex-col gap-2">
              <Button
                type="button"
                variant="outline"
                disabled={phase === 'working'}
                onClick={() => void shareImage()}
              >
                {t.chart.shareImage}
              </Button>
              <Button
                type="button"
                variant="outline"
                disabled={phase === 'working'}
                onClick={() => void createLink()}
              >
                {t.chart.shareLink}
              </Button>
            </div>
          )}

          {/*
            One live region, always mounted.

            Rendering it only when there is something to say means the
            region itself is new when the text appears, and a region that
            did not exist a moment ago is not reliably announced.
          */}
          <p
            role="status"
            aria-live="polite"
            className={cn(
              'min-h-[1.25rem] text-xs',
              phase === 'failed' || phase === 'too-many' ? 'text-danger' : 'text-ink-muted',
            )}
          >
            {phase === 'failed' && t.chart.shareFailed}
            {phase === 'too-many' && t.chart.shareTooMany}
          </p>
        </DialogContent>
      </Dialog>
    </>
  )
}

function LinkPanel({
  link,
  copied,
  onCopy,
  copyLabel,
  readyLabel,
  expiryLabel,
}: {
  link: string
  copied: boolean
  onCopy: () => void
  copyLabel: string
  readyLabel: string
  expiryLabel: string
}) {
  return (
    <div className="flex flex-col gap-2">
      <p className="text-sm font-medium text-ink">{readyLabel}</p>

      <div className="flex gap-2">
        {/*
          readOnly, not disabled.

          A disabled input cannot be focused, selected or read by a screen
          reader in several browsers — which would leave somebody whose
          clipboard permission was refused with no way at all to get the
          link out.
        */}
        <input
          readOnly
          value={link}
          aria-label={readyLabel}
          onFocus={(event) => event.currentTarget.select()}
          className={cn(
            'min-w-0 flex-1 rounded-md border border-input bg-elevated px-2 py-1',
            'text-xs text-ink focus-visible:outline-none focus-visible:ring-2',
            'focus-visible:ring-ring',
          )}
        />
        <Button type="button" variant="outline" size="sm" onClick={onCopy}>
          {/* A glyph as well as the word: colour and wording alone are
              not enough to signal state at a glance. */}
          <span aria-hidden="true" className="mr-1">
            {copied ? '✓' : '⧉'}
          </span>
          {copyLabel}
        </Button>
      </div>

      <p className="text-xs text-ink-muted">{expiryLabel}</p>
    </div>
  )
}

/**
 * The 409 the API returns when an account has too many live links.
 *
 * Matched on the status rather than the message, because the message is
 * user-facing copy and will be reworded.
 */
function isConflict(err: unknown): boolean {
  return (
    typeof err === 'object' &&
    err !== null &&
    'status' in err &&
    (err as { status?: number }).status === 409
  )
}

function downloadBlob(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = filename
  document.body.appendChild(anchor)
  anchor.click()
  anchor.remove()
  URL.revokeObjectURL(url)
}
