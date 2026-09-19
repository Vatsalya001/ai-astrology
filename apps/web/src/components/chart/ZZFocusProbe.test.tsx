import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it } from 'vitest'

import { DashaTimeline, type TimelineLevel } from './DashaTimeline'
import { LocaleProvider } from '@/lib/i18n/context'

const YEAR = 365.25 * 24 * 60 * 60 * 1000
const IN_JUPITER = Date.UTC(2064, 0, 1)

function mahadashas() {
  let cursor = Date.UTC(1994, 0, 1)
  return [
    ['Ketu', 7],
    ['Venus', 20],
    ['Sun', 6],
    ['Moon', 10],
    ['Mars', 7],
    ['Rahu', 18],
    ['Jupiter', 16],
    ['Saturn', 19],
    ['Mercury', 17],
  ].map(([planet, years], i) => {
    const start = cursor
    cursor += (years as number) * YEAR
    return {
      id: `maha-${i}`,
      planet: planet as string,
      start: new Date(start).toISOString(),
      end: new Date(cursor).toISOString(),
    }
  })
}

/**
 * Mirrors dashas/page.tsx: onRetryLevel -> drillDown, which synchronously
 * sets { periods: [], loading: true } for that level.
 */
function Harness() {
  const [levels, setLevels] = useState<TimelineLevel[]>([
    { periods: mahadashas(), loading: false },
    { periods: [], loading: false, failed: true },
    { periods: [], loading: false },
  ])

  function drillDown(level: number) {
    setLevels((prev) => {
      const next = [...prev]
      next[level] = { periods: [], loading: true }
      for (let i = level + 1; i < next.length; i++) next[i] = { periods: [], loading: false }
      return next
    })
  }

  return (
    <LocaleProvider>
      <DashaTimeline
        levels={levels}
        currentAt={IN_JUPITER}
        onDrillDown={() => {}}
        onRetryLevel={(level) => drillDown(level)}
      />
    </LocaleProvider>
  )
}

function describeActive() {
  const el = document.activeElement
  if (!el) return 'NONE'
  if (el === document.body) return 'BODY'
  return `${el.tagName}: ${(el.textContent ?? '').trim().slice(0, 40)}`
}

describe('PROBE: focus after pressing Try again', () => {
  it('reports where focus lands', async () => {
    const user = userEvent.setup()
    render(<Harness />)

    const retry = screen.getByRole('button', { name: /try again/i })
    retry.focus()
    console.log('BEFORE CLICK:', describeActive())
    expect(document.activeElement).toBe(retry)

    await user.click(retry)

    console.log('AFTER CLICK:', describeActive())
    console.log('BUTTON STILL IN DOM:', document.body.contains(retry))
    console.log(
      'ANY BUTTON IN LEVEL-1 REGION:',
      screen.queryAllByRole('button', { name: /try again/i }).length,
    )
    console.log('FULL DOM AFTER:\n', document.body.innerHTML)
  })
})
