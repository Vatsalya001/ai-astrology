'use client'

import type { BirthProfile } from '@/lib/astrology-api'
import { useSelectedProfile } from '@/lib/profile-context'
import { cn } from '@/lib/utils'

/**
 * Switch between the charts a user holds — self, partner, child.
 *
 * ── A native select ──
 *
 * Not a custom dropdown. A `<select>` gets keyboard behaviour, the
 * platform's own picker on a phone, screen-reader support and typeahead
 * for free, and every one of those is something a hand-rolled menu has
 * to reimplement and usually reimplements incompletely. The list is
 * short and unstyled-per-item; there is nothing here a custom control
 * would buy.
 *
 * ── Hidden with one profile, not disabled ──
 *
 * A switcher offering one choice is furniture. It renders nothing until
 * there are two, which is also why the label says "chart" rather than
 * naming the person: the first profile is usually the user themselves
 * and "Priya ▾" in a header reads like an account menu.
 *
 * ── No birth details in the option text ──
 *
 * Only the label. Date plus time plus place is, in combination, close to
 * a unique identifier, and this control renders on every Kundli screen
 * and into the PDF — so it is exactly the kind of place that quietly
 * becomes a screenshot in a support ticket.
 */
export function ProfileSwitcher({
  profiles,
  className,
}: {
  profiles: BirthProfile[]
  className?: string
}) {
  const { selectedId, select } = useSelectedProfile()

  if (profiles.length < 2) return null

  // The same fallback `resolveProfile` applies, so the control shows the
  // profile the screen is actually rendering rather than a stale id that
  // matches nothing in the list.
  const current = profiles.find((p) => p.id === selectedId)?.id ?? profiles[0]!.id

  return (
    <div className={cn('flex items-center gap-2', className)}>
      <label htmlFor="profile-switcher" className="text-xs text-ink-muted">
        Chart
      </label>
      <select
        id="profile-switcher"
        value={current}
        onChange={(e) => select(e.target.value)}
        className={cn(
          'rounded-lg border border-input bg-surface px-3 py-1.5 text-sm text-ink',
          'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
          'focus-visible:ring-offset-2 focus-visible:ring-offset-background',
        )}
      >
        {profiles.map((profile, i) => (
          <option key={profile.id} value={profile.id}>
            {profile.label?.trim() || `Chart ${i + 1}`}
          </option>
        ))}
      </select>
    </div>
  )
}
