'use client'

import dynamic from 'next/dynamic'
import { useSearchParams } from 'next/navigation'
import { Suspense } from 'react'

/**
 * Mounts the accessibility inspector when the URL asks for it.
 *
 * ── Why dynamic, and why a query parameter ──
 *
 * `next/dynamic` with `ssr: false` puts the panel in its own chunk,
 * loaded only when it is actually wanted. That keeps it out of the
 * first-load JS that `bundle-budget.json` enforces — a developer tool
 * that costs every user on a 4G connection is one that gets deleted
 * rather than used.
 *
 * A query parameter rather than an env flag, because the person who
 * needs it is testing a REAL build: `next start`, the same bundle that
 * ships. An env-gated tool is absent from exactly the build worth
 * inspecting.
 */
const A11yInspector = dynamic(
  () => import('./A11yInspector').then((m) => m.A11yInspector),
  { ssr: false },
)

function Gate() {
  const params = useSearchParams()
  if (params.get('a11y') !== '1') return null
  return <A11yInspector />
}

export function A11yInspectorMount() {
  // useSearchParams needs a Suspense boundary above it, or `next build`
  // refuses the route.
  return (
    <Suspense fallback={null}>
      <Gate />
    </Suspense>
  )
}
