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
 * A React node flattened to the string a screen reader would hear.
 *
 * `children` here is always a string or a number in practice — a
 * nakshatra, a house ordinal, a dignity — but the prop type permits a
 * node, and a label built by interpolating `[object Object]` into speech
 * would be worse than the bug it replaces. Anything that is not plain
 * text yields '', which makes the caller fall back to the plain
 * affordance label rather than announce nonsense.
 */
function flatten(node: ReactNode): string {
  if (typeof node === 'string') return node.trim()
  if (typeof node === 'number') return String(node)
  if (Array.isArray(node)) return node.map(flatten).filter(Boolean).join(' ')
  return ''
}

/**
 * The accessible name for the glossary control.
 *
 * ── Why the value goes first ──
 *
 * `aria-label` wins accname over name-from-content, so a bare
 * "What “{term}” means" REPLACES whatever the button wraps. That is
 * correct where the children ARE the term and destroys data where they
 * are a value: every Nakshatra cell on /kundli/planets announced
 * "What “Nakshatra” means" instead of "Purva Ashadha 3".
 *
 * ── Why the equality check exists ──
 *
 * The first fix prefixed unconditionally, and immediately produced
 * `button "Rasi. What “Rasi” means"` on the chart page — a stutter,
 * because <AstroTerm term="rasi">Rasi</AstroTerm> passes children that
 * are the term's own name. The test that was supposed to guard this
 * rendered <AstroTerm term="nakshatra" /> with NO children, so it
 * exercised the `entry.name` fallback and never saw the case that broke.
 * A guard aimed one branch away from the defect.
 *
 * Compared case- and punctuation-insensitively: "Rasi" and "rasi" are
 * the same word said twice, and so are "First house" and "first house".
 */
function prefixWithValue(value: string, termName: string, template: string): string {
  // Substituted here rather than through the context's `fill`, so the
  // accessible name is testable without standing up a locale provider.
  const affordance = template.replace('{term}', termName)
  if (!value) return affordance

  const same = (a: string) => a.toLowerCase().replace(/[^a-z0-9]/g, '')
  if (same(value) === same(termName)) return affordance

  return `${value}. ${affordance}`
}

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
        /*
          The label must CARRY the children, not replace them.

          It was `aria-label={interpolateDefine(…, entry.name)}`
          unconditionally — "What “Nakshatra” means" — which is right
          where the children ARE the term ("Nakshatra", "First house":
          nothing is lost) and destroys data where they are a VALUE.

          On /kundli/planets the children are the value, so every one of
          the nine Nakshatra cells announced "What “Nakshatra” means"
          instead of "Purva Ashadha 3". Confirmed against Playwright's
          accname implementation: the Moon's row name came out as
          `Moon in Sagittarius, 4th house, 21 degrees Sagittarius 20°44'
          4th What “Nakshatra” means —` — the nakshatra absent from the
          row entirely. It is not derivable by ear from the sign and
          degree that ARE announced, and a voice-control user saying
          "click Purva Ashadha 3" hit nothing.

          Line 65 already draws this distinction for the visible text
          (`children ?? entry.name`); the label did not. Now the value
          comes first and the affordance follows, so a reader hears the
          data and then what the control does.

          axe cannot see this: it checks that a name EXISTS, never that
          it preserves what it replaced. The rule that would catch it,
          `label-content-name-mismatch`, is experimental and outside the
          wcag2a/2aa tag sets this suite runs.
        */
        aria-label={prefixWithValue(flatten(children), entry.name, t.chart.defineTerm)}
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
