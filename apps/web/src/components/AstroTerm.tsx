'use client'

import { useState, type ReactNode } from 'react'
import { defineTerm } from '@ayana/content'

import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { track } from '@/lib/analytics'
import { useLocale } from '@/lib/i18n/context'
import { cn } from '@/lib/utils'

/**
 * A word in the chart UI that most users will not know, made tappable.
 *
 * "Combust", "Sade Sati", "Vimshottari", "navamsa" — a chart is dense
 * with vocabulary, and the difference between a screen that impresses
 * and one that intimidates is whether the reader can find out what a
 * word means without leaving the page.
 *
 * ── Why a dialog and not a tooltip ──
 *
 * A `title` attribute and a hover card both require a pointer. On a
 * phone — the majority case for this product — hover does not exist,
 * and `title` is either unreachable or appears after a long press that
 * competes with text selection. So the interaction is a tap that opens
 * a sheet, which works identically with a mouse, a finger, a keyboard
 * and a screen reader.
 *
 * ── Why an unknown term renders as plain text ──
 *
 * `defineTerm` returns `null` rather than throwing or echoing the key.
 * This component honours that: no definition means no button, and the
 * children render exactly as they would have without the wrapper. A
 * missing entry costs the reader a tooltip; it never costs them the
 * sentence they were reading.
 *
 * That silence is deliberate but it is not free, which is why
 * `AstroTerm.test.tsx` asserts the corpus covers every locale the
 * language picker can offer. Without that guard, adding a locale would
 * turn every term on every screen into plain text with nothing in the
 * console to say so.
 */
export function AstroTerm({
  term,
  children,
  className,
}: {
  /** A key in the glossary. Unknown keys degrade to plain text. */
  term: string
  /** What to show inline. Defaults to the term's localised name. */
  children?: ReactNode
  className?: string
}) {
  const { locale, t } = useLocale()
  const [open, setOpen] = useState(false)

  const entry = defineTerm(term, locale)
  if (!entry) return <>{children}</>

  const label = children ?? entry.name

  return (
    <>
      <button
        type="button"
        onClick={() => {
          // Which words readers actually tap is the only evidence for
          // which glossary entries are worth expanding — and, in Phase
          // 5, which terms the model should explain unprompted.
          track('glossary_term_opened', { term_key: term })
          setOpen(true)
        }}
        className={cn(
          // A dotted underline rather than colour alone. Roughly 8% of
          // men cannot distinguish the gold from the body text, and a
          // word that looks tappable only to people with full colour
          // vision is a word most readers never tap.
          'inline underline decoration-dotted decoration-primary/70 underline-offset-4',
          'text-left text-inherit transition-colors hover:decoration-primary',
          'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
          'focus-visible:ring-offset-2 focus-visible:ring-offset-background rounded-sm',
          className,
        )}
        // Without this a screen reader announces "Nakshatra, button" and
        // the user has no idea what pressing it does.
        aria-label={interpolateDefine(t.chart.defineTerm, entry.name)}
      >
        {label}
      </button>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>{entry.name}</DialogTitle>
            <DialogDescription>{entry.short}</DialogDescription>
          </DialogHeader>
          <p className="text-sm leading-relaxed text-foreground">{entry.long}</p>
        </DialogContent>
      </Dialog>
    </>
  )
}

/**
 * Local rather than the context's `fill`.
 *
 * `fill` is reached through `useLocale()`, which is fine, but this is
 * the only substitution the component makes and keeping it here means
 * the aria-label is testable without standing up a provider.
 */
function interpolateDefine(template: string, name: string): string {
  return template.replace('{term}', name)
}
