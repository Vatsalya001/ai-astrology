'use client'

import { useId, useMemo } from 'react'

import {
  glyphPosition,
  houseOfSign,
  northIndianHouses,
  signOfHouse,
  SIZE,
  southIndianCentre,
  southIndianSigns,
  toPoints,
  type ChartStyle,
  type HouseNumber,
  type Cell,
  type SignIndex,
} from '@ayana/astrology-geometry'

import { cn } from '@/lib/utils'

import { planetAbbreviation, planetLabel, ordinal, RETROGRADE_MARK } from './glyphs'
import { highlightedHouse, type ChartData, type Highlight, type PlanetPlacement } from './types'

/**
 * The birth chart, as SVG.
 *
 * SVG and not an image or a canvas, for the reasons the spec gives: it
 * stays crisp at every size and DPI, it prints correctly into the PDF,
 * every glyph is a real DOM node that can be tapped and labelled, and it
 * themes from CSS variables so there is no second palette to keep in
 * step.
 *
 * ── On accessibility, and one deliberate deviation ──
 *
 * The spec asks for `role="img"` with a full label AND for each planet
 * glyph to be a focusable `role="button"`. Those two cannot both hold:
 * `role="img"` is a leaf in the accessibility tree, so its children —
 * including any buttons — are not exposed at all. Following both
 * literally produces a chart whose buttons no screen reader can reach.
 *
 * What is implemented instead:
 *
 *   - the `<svg>` is a `role="group"` carrying the summary sentence, so
 *     its children stay in the tree
 *   - each planet is a focusable `role="button"` with the full sentence
 *     as its label, which is the spec's intent
 *   - a visually-hidden `<table>` duplicates the data, captioned as such
 *     so the repetition reads as deliberate rather than as a bug
 *
 * A screen-reader user can therefore either tab the chart like a set of
 * controls or read the table with table navigation, whichever suits.
 * Both were in the spec; only the `role="img"` wrapper had to go.
 */
export function ChartSVG({
  chart,
  style,
  highlight = [],
  onPlanetTap,
  onHouseTap,
  className,
  title,
}: {
  chart: ChartData
  style: ChartStyle
  highlight?: Highlight[]
  onPlanetTap?: (planet: PlanetPlacement) => void
  onHouseTap?: (house: HouseNumber) => void
  className?: string
  title?: string
}) {
  const summaryId = useId()
  const ascendantSign = chart.ascendant?.signIndex ?? null

  const highlightedPlanets = useMemo(
    () => new Set(highlight.filter((value) => !value.startsWith('house:'))),
    [highlight],
  )
  const highlightedHouses = useMemo(
    () =>
      new Set(
        highlight
          .map(highlightedHouse)
          .filter((house): house is number => house !== null),
      ),
    [highlight],
  )

  // Group the planets by the region they are drawn in. North Indian
  // regions are houses; South Indian regions are signs. Doing it once
  // here rather than filtering inside each cell keeps the render O(n)
  // instead of O(n × 12), which matters on the PDF's twelve charts.
  const byRegion = useMemo(() => {
    const map = new Map<number, PlanetPlacement[]>()
    for (const planet of chart.planets) {
      // A planet with no house cannot be placed in a North Indian
      // chart, whose cells ARE the houses. It is skipped rather than
      // bucketed under 0, which would collect every such planet into a
      // cell that does not exist. The South Indian chart keys on the
      // sign and is unaffected — which is exactly why it is the style
      // that still works without a birth time.
      const key = style === 'north' ? planet.house : planet.signIndex
      if (key === null) continue

      const bucket = map.get(key)
      if (bucket) bucket.push(planet)
      else map.set(key, [planet])
    }
    return map
  }, [chart.planets, style])

  // Without a birth time there is no ascendant, so a North Indian chart
  // — whose whole structure is houses counted from it — cannot be drawn
  // at all. Say so rather than render an empty diamond that looks like a
  // loading state or a bug.
  if (style === 'north' && ascendantSign === null) {
    return (
      <p className={cn('rounded-xl border border-border bg-surface p-6 text-sm', className)}>
        A North Indian chart is drawn from the rising sign, which needs an exact birth
        time. Your planetary positions are still shown below, and the South Indian chart
        works without one.
      </p>
    )
  }

  return (
    <figure className={cn('relative', className)}>
      <svg
        viewBox={`0 0 ${SIZE} ${SIZE}`}
        role="group"
        aria-labelledby={summaryId}
        className="w-full select-none"
        // Text scales with the box rather than with the root font size,
        // so the chart is legible at 360px and in the PDF without two
        // sets of sizes.
        style={{ fontSize: `${SIZE * 0.04}px` }}
      >
        <rect
          x={0}
          y={0}
          width={SIZE}
          height={SIZE}
          className="fill-surface stroke-border"
          strokeWidth={0.5}
        />

        {style === 'north'
          ? renderNorth({
              ascendantSign: ascendantSign!,
              byRegion,
              highlightedPlanets,
              highlightedHouses,
              onPlanetTap,
              onHouseTap,
            })
          : renderSouth({
              ascendantSign,
              byRegion,
              highlightedPlanets,
              highlightedHouses,
              onPlanetTap,
              onHouseTap,
            })}
      </svg>

      <p id={summaryId} className="sr-only">
        {summarise(chart, style, title)}
      </p>

      <ChartDataTable chart={chart} />
    </figure>
  )
}

// ─── North Indian ────────────────────────────────────────────────────

function renderNorth({
  ascendantSign,
  byRegion,
  highlightedPlanets,
  highlightedHouses,
  onPlanetTap,
  onHouseTap,
}: {
  ascendantSign: SignIndex
  byRegion: Map<number, PlanetPlacement[]>
  highlightedPlanets: Set<string>
  highlightedHouses: Set<number>
  onPlanetTap?: (planet: PlanetPlacement) => void
  onHouseTap?: (house: HouseNumber) => void
}) {
  return northIndianHouses().map((cell) => {
    const sign = signOfHouse(cell.house, ascendantSign)
    const planets = byRegion.get(cell.house) ?? []
    const marked = highlightedHouses.has(cell.house)

    return (
      <g key={cell.house}>
        <polygon
          points={toPoints(cell.polygon)}
          className={cn(
            'stroke-border transition-colors',
            marked ? 'fill-gold/15' : 'fill-transparent',
          )}
          strokeWidth={0.4}
        />

        <RegionTap
          cell={cell}
          label={`${ordinal(cell.house)} house, ${signName(sign)}`}
          onTap={onHouseTap ? () => onHouseTap(cell.house) : undefined}
        />

        {/* The sign NUMBER, which is what a North Indian chart shows in
            each house rather than the sign name — there is no room for
            "Sagittarius" in a corner triangle, and readers of this
            style read the number. 1 = Aries. */}
        <text
          x={cell.label.x}
          y={cell.label.y}
          textAnchor="middle"
          dominantBaseline="middle"
          className="fill-ink-faint"
          style={{ fontSize: `${SIZE * 0.032}px` }}
          aria-hidden="true"
        >
          {sign + 1}
        </text>

        <PlanetGlyphs
          cell={cell}
          planets={planets}
          style="north"
          highlighted={highlightedPlanets}
          onPlanetTap={onPlanetTap}
        />
      </g>
    )
  })
}

// ─── South Indian ────────────────────────────────────────────────────

function renderSouth({
  ascendantSign,
  byRegion,
  highlightedPlanets,
  highlightedHouses,
  onPlanetTap,
  onHouseTap,
}: {
  ascendantSign: SignIndex | null
  byRegion: Map<number, PlanetPlacement[]>
  highlightedPlanets: Set<string>
  highlightedHouses: Set<number>
  onPlanetTap?: (planet: PlanetPlacement) => void
  onHouseTap?: (house: HouseNumber) => void
}) {
  const centre = southIndianCentre()

  return (
    <>
      {/* The hollow middle, drawn as the page colour so the grid reads
          as a ring rather than as four blank cells. */}
      <polygon points={toPoints(centre)} className="fill-base stroke-border" strokeWidth={0.4} />

      {southIndianSigns().map((cell) => {
        const house = ascendantSign === null ? null : houseOfSign(cell.sign, ascendantSign)
        const planets = byRegion.get(cell.sign) ?? []
        const isAscendant = ascendantSign !== null && cell.sign === ascendantSign
        const marked = house !== null && highlightedHouses.has(house)

        return (
          <g key={cell.sign}>
            <polygon
              points={toPoints(cell.polygon)}
              className={cn(
                'stroke-border transition-colors',
                marked ? 'fill-gold/15' : 'fill-transparent',
              )}
              strokeWidth={0.4}
            />

            {/* The ascendant cell is marked with a diagonal, which is
                the convention — and a shape rather than a colour, so it
                survives a monochrome print and colour blindness. */}
            {isAscendant && (
              <line
                x1={cell.polygon[0]!.x}
                y1={cell.polygon[0]!.y}
                x2={cell.polygon[2]!.x}
                y2={cell.polygon[2]!.y}
                className="stroke-gold"
                strokeWidth={0.6}
                aria-hidden="true"
              />
            )}

            <RegionTap
              cell={cell}
              label={
                house === null
                  ? signName(cell.sign)
                  : `${signName(cell.sign)}, ${ordinal(house)} house`
              }
              onTap={onHouseTap && house !== null ? () => onHouseTap(house as HouseNumber) : undefined}
            />

            <text
              x={cell.label.x}
              y={cell.label.y}
              className="fill-ink-faint"
              style={{ fontSize: `${SIZE * 0.028}px` }}
              aria-hidden="true"
            >
              {signName(cell.sign).slice(0, 3)}
            </text>

            <PlanetGlyphs
              cell={cell}
              planets={planets}
              style="south"
              highlighted={highlightedPlanets}
              onPlanetTap={onPlanetTap}
            />
          </g>
        )
      })}
    </>
  )
}

// ─── shared pieces ───────────────────────────────────────────────────

/**
 * An invisible hit area covering a whole region.
 *
 * Rendered only when there is something to do with a tap. A focusable
 * element that does nothing is a tab stop a keyboard user pays for and
 * gets nothing back from.
 */
function RegionTap({
  cell,
  label,
  onTap,
}: {
  cell: Cell
  label: string
  onTap?: () => void
}) {
  if (!onTap) return null

  return (
    <polygon
      points={toPoints(cell.polygon)}
      className="cursor-pointer fill-transparent focus-visible:outline-none"
      role="button"
      tabIndex={0}
      aria-label={label}
      onClick={onTap}
      onKeyDown={(event) => {
        // Enter and Space, because a role="button" has to behave like
        // one. A polygon gets neither for free.
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault()
          onTap()
        }
      }}
    />
  )
}

function PlanetGlyphs({
  cell,
  planets,
  style,
  highlighted,
  onPlanetTap,
}: {
  cell: Cell
  planets: PlanetPlacement[]
  style: ChartStyle
  highlighted: Set<string>
  onPlanetTap?: (planet: PlanetPlacement) => void
}) {
  return (
    <>
      {planets.map((planet, index) => {
        const at = glyphPosition(cell, index, style)
        const marked = highlighted.has(planet.planet)
        const interactive = Boolean(onPlanetTap)

        return (
          <g
            key={planet.planet}
            role={interactive ? 'button' : undefined}
            tabIndex={interactive ? 0 : undefined}
            aria-label={interactive ? planetLabel(planet) : undefined}
            className={cn(interactive && 'cursor-pointer')}
            onClick={onPlanetTap ? () => onPlanetTap(planet) : undefined}
            onKeyDown={
              onPlanetTap
                ? (event) => {
                    if (event.key === 'Enter' || event.key === ' ') {
                      event.preventDefault()
                      onPlanetTap(planet)
                    }
                  }
                : undefined
            }
          >
            {/* Combustion is a ring, not a colour. Same reasoning as the
                retrograde glyph: it changes how a placement is read, so
                it cannot be carried by hue alone. */}
            {planet.isCombust && (
              <circle
                cx={at.x + SIZE * 0.012}
                cy={at.y - SIZE * 0.004}
                r={SIZE * 0.022}
                className="fill-none stroke-warn"
                strokeWidth={0.35}
                aria-hidden="true"
              />
            )}

            <text
              x={at.x}
              y={at.y}
              className={cn(
                'transition-colors',
                marked ? 'fill-gold font-semibold' : 'fill-ink',
              )}
              aria-hidden="true"
            >
              {planetAbbreviation(planet.planet)}
              {planet.isRetrograde && (
                <tspan className="fill-ink-muted" style={{ fontSize: `${SIZE * 0.028}px` }}>
                  {RETROGRADE_MARK}
                </tspan>
              )}
            </text>
          </g>
        )
      })}
    </>
  )
}

/**
 * The chart as a table, for screen readers.
 *
 * Captioned as "the same data as the chart above" so the duplication is
 * obviously intentional. Without the caption a screen-reader user meets
 * the same nine planets twice and reasonably concludes something is
 * broken.
 */
function ChartDataTable({ chart }: { chart: ChartData }) {
  return (
    <table className="sr-only">
      <caption>Planetary positions — the same data as the chart above, as a table.</caption>
      <thead>
        <tr>
          <th scope="col">Planet</th>
          <th scope="col">Sign</th>
          <th scope="col">House</th>
          <th scope="col">Degree</th>
          <th scope="col">Nakshatra</th>
          <th scope="col">Notes</th>
        </tr>
      </thead>
      <tbody>
        {chart.ascendant && (
          <tr>
            <th scope="row">Ascendant</th>
            <td>{chart.ascendant.sign}</td>
            <td>1st</td>
            <td>{Math.round(chart.ascendant.degree)} degrees</td>
            <td>
              {chart.ascendant.nakshatra}, pada {chart.ascendant.pada}
            </td>
            <td />
          </tr>
        )}
        {chart.planets.map((planet) => (
          <tr key={planet.planet}>
            <th scope="row">{planet.planet}</th>
            <td>{planet.sign}</td>
            <td>{planet.house === null ? '—' : ordinal(planet.house)}</td>
            <td>{Math.round(planet.degree)} degrees</td>
            <td>
              {planet.nakshatra}, pada {planet.pada}
            </td>
            <td>
              {[
                planet.isRetrograde ? 'retrograde' : null,
                planet.isCombust ? 'combust' : null,
                planet.dignity !== 'neutral' ? planet.dignity : null,
              ]
                .filter(Boolean)
                .join(', ')}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

const SIGN_NAMES = [
  'Aries', 'Taurus', 'Gemini', 'Cancer', 'Leo', 'Virgo',
  'Libra', 'Scorpio', 'Sagittarius', 'Capricorn', 'Aquarius', 'Pisces',
] as const

function signName(sign: SignIndex): string {
  return SIGN_NAMES[sign]
}

/**
 * One sentence describing the whole chart.
 *
 * What a sighted person takes from a glance: the style, the rising sign,
 * and where the luminaries are. The table carries the detail, so this
 * stays short — a screen reader announcing forty facts before the user
 * has asked for any is worse than announcing three.
 */
function summarise(chart: ChartData, style: ChartStyle, title?: string): string {
  const styleName = style === 'north' ? 'North Indian' : 'South Indian'
  const parts = [title ?? `${styleName} birth chart`]

  if (chart.ascendant) {
    parts.push(`${chart.ascendant.sign} rising`)
  } else {
    parts.push('rising sign unknown, no birth time recorded')
  }

  for (const name of ['Sun', 'Moon']) {
    const planet = chart.planets.find((candidate) => candidate.planet === name)
    if (planet) parts.push(`${name} in ${planet.sign}`)
  }

  parts.push(`${chart.planets.length} planets placed`)
  return `${parts.join('. ')}.`
}
